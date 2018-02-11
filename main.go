package main

import (
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"os"

	"golang.org/x/time/rate"

	socket "github.com/akillmer/go-socket"
	db "github.com/akillmer/riptide/database"
	"github.com/akillmer/riptide/queue"
	"github.com/anacrolix/dht"
	"github.com/anacrolix/torrent"
)

var (
	client      *torrent.Client
	globalRatio float64
	downloadDir string
)

// InitClientData is sent to every client that connects
type InitClientData struct {
	Torrents []*TorrentInfo `json:"torrents"`
	Labels   []*Label       `json:"labels"`
}

func main() {
	var (
		maxDownloadSpeed  int
		maxUploadSpeed    int
		maxActiveTorrents int
		devmode           bool
		servePort         string
		appDir            string
	)

	flag.StringVar(&downloadDir, "downloads", "./downloads", "directory for downloading torrents")
	flag.IntVar(&maxActiveTorrents, "max", 1, "maximum number of active torrents")
	flag.Float64Var(&globalRatio, "ratio", 1.0, "global ratio for all torrents (0: no seeding, -1: unlimited)")
	flag.BoolVar(&devmode, "devmode", false, "development mode")
	flag.IntVar(&maxDownloadSpeed, "dl", 0, "maximum download speed in KB/s")
	flag.IntVar(&maxUploadSpeed, "ul", 0, "maximum upload speed in KB/s")
	flag.StringVar(&servePort, "port", "6500", "listening port for riptide clients")
	flag.StringVar(&appDir, "app", "./app", "directory for serving static react app")
	flag.Parse()

	if err := db.Open("./.riptide.bolt.db"); err != nil {
		log.Fatalf("failed to open riptide.db: %v", err)
	}

	cfg := &torrent.Config{
		DataDir: downloadDir,
		DHTConfig: dht.ServerConfig{
			StartingNodes: dht.GlobalBootstrapAddrs,
		},
		//DefaultStorage: storage.NewMMap(downloadDir),
	}

	if globalRatio == 0 {
		cfg.Seed = false
	}

	if maxDownloadSpeed > 0 {
		limit := rate.Limit(maxDownloadSpeed << 10)
		cfg.DownloadRateLimiter = rate.NewLimiter(limit, 32<<10)
	}

	if maxUploadSpeed > 0 {
		limit := rate.Limit(maxUploadSpeed << 10)
		cfg.UploadRateLimiter = rate.NewLimiter(limit, 32<<10)
	}

	if c, err := torrent.NewClient(cfg); err != nil {
		log.Fatalf("failed to create torrent client: %v", err)
	} else {
		client = c
	}

	socket.OnOpen = initDataWithClient
	socket.OnError = func(clientID string, err error) {
		log.Printf("%s: %v", clientID, err)
	}
	if devmode {
		log.Println("Riptide mode: development")
		socket.CheckOrigin = func(r *http.Request) bool {
			return true
		}

		servePort = "9800"
	} else {
		http.Handle("/", http.FileServer(http.Dir(appDir)))
	}

	bootstrapTorrents()
	go handleAPI()
	go queue.Run(maxActiveTorrents)
	go func() {
		for {
			hash := queue.Next()
			go startTorrent(hash)
		}
	}()

	http.HandleFunc("/api", socket.Handler)
	log.Printf("Now serving on http://localhost:%s", servePort)

	os.Remove("riptide.log")
	f, err := os.OpenFile("riptide.log", os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		log.Fatalf("error opening log file: %v", err)
	}
	defer f.Close()

	log.Printf("Saving log to riptide.log")
	log.SetOutput(f)

	log.Fatal(http.ListenAndServe(":"+servePort, nil))
}

func initDataWithClient(clientID string) {
	init := &InitClientData{}

	for _, buf := range db.All(db.BucketTorrents) {
		t := &TorrentInfo{}
		if err := json.Unmarshal(buf, t); err != nil {
			log.Fatalf("failed to init torrents for new client: %v", err)
		} else {
			init.Torrents = append(init.Torrents, t)
		}
	}

	for _, buf := range db.All(db.BucketLabels) {
		l := &Label{}
		if err := json.Unmarshal(buf, l); err != nil {
			log.Fatalf("failed to init labels for new client: %v", err)
		} else {
			init.Labels = append(init.Labels, l)
		}
	}

	socket.Send(clientID, MsgClientInit, init)
}

func bootstrapTorrents() {
	for _, buf := range db.All(db.BucketTorrents) {
		info := &TorrentInfo{}
		if err := json.Unmarshal(buf, info); err != nil {
			log.Fatalf("failed to restore saved torrent: %v", err)
		}

		switch info.Status {
		case StatusActive:
			queue.ForceNext(info.Hash)
		case StatusPending:
			// this is a pretty narrow case: a torrent has StatusPending before it ever reaches
			// the Queue (where then status then changes)
			if err := addTorrentByMagnet(info.Magnet); err != nil {
				log.Fatalf("failed to add pending torrent by magnet: %v", err)
			}
		default:
			break
		}
	}
}
