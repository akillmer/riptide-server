// Copyright (C) 2015 Audrius Butkevicius and Contributors.

package main

import (
	"crypto/tls"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/syncthing/syncthing/lib/osutil"
	"github.com/syncthing/syncthing/lib/relay/protocol"
	"github.com/syncthing/syncthing/lib/tlsutil"
	"golang.org/x/time/rate"

	"github.com/syncthing/syncthing/lib/config"
	"github.com/syncthing/syncthing/lib/nat"
	_ "github.com/syncthing/syncthing/lib/pmp"
	_ "github.com/syncthing/syncthing/lib/upnp"

	syncthingprotocol "github.com/syncthing/syncthing/lib/protocol"
)

var (
	Version    string
	BuildStamp string
	BuildUser  string
	BuildHost  string

	BuildDate   time.Time
	LongVersion string
)

func init() {
	stamp, _ := strconv.Atoi(BuildStamp)
	BuildDate = time.Unix(int64(stamp), 0)

	date := BuildDate.UTC().Format("2006-01-02 15:04:05 MST")
	LongVersion = fmt.Sprintf(`strelaysrv %s (%s %s-%s) %s@%s %s`, Version, runtime.Version(), runtime.GOOS, runtime.GOARCH, BuildUser, BuildHost, date)
}

var (
	listen string
	debug  bool

	sessionAddress []byte
	sessionPort    uint16

	networkTimeout = 2 * time.Minute
	pingInterval   = time.Minute
	messageTimeout = time.Minute

	limitCheckTimer *time.Timer

	sessionLimitBps   int
	globalLimitBps    int
	overLimit         int32
	descriptorLimit   int64
	sessionLimiter    *rate.Limiter
	globalLimiter     *rate.Limiter
	networkBufferSize int

	statusAddr       string
	poolAddrs        string
	pools            []string
	providedBy       string
	defaultPoolAddrs = "https://relays.syncthing.net/endpoint"

	natEnabled bool
	natLease   int
	natRenewal int
	natTimeout int

	pprofEnabled bool
)

func main() {
	log.SetFlags(log.Lshortfile | log.LstdFlags)

	var dir, extAddress, proto string

	flag.StringVar(&listen, "listen", ":22067", "Protocol listen address")
	flag.StringVar(&dir, "keys", ".", "Directory where cert.pem and key.pem is stored")
	flag.DurationVar(&networkTimeout, "network-timeout", networkTimeout, "Timeout for network operations between the client and the relay.\n\tIf no data is received between the client and the relay in this period of time, the connection is terminated.\n\tFurthermore, if no data is sent between either clients being relayed within this period of time, the session is also terminated.")
	flag.DurationVar(&pingInterval, "ping-interval", pingInterval, "How often pings are sent")
	flag.DurationVar(&messageTimeout, "message-timeout", messageTimeout, "Maximum amount of time we wait for relevant messages to arrive")
	flag.IntVar(&sessionLimitBps, "per-session-rate", sessionLimitBps, "Per session rate limit, in bytes/s")
	flag.IntVar(&globalLimitBps, "global-rate", globalLimitBps, "Global rate limit, in bytes/s")
	flag.BoolVar(&debug, "debug", debug, "Enable debug output")
	flag.StringVar(&statusAddr, "status-srv", ":22070", "Listen address for status service (blank to disable)")
	flag.StringVar(&poolAddrs, "pools", defaultPoolAddrs, "Comma separated list of relay pool addresses to join")
	flag.StringVar(&providedBy, "provided-by", "", "An optional description about who provides the relay")
	flag.StringVar(&extAddress, "ext-address", "", "An optional address to advertise as being available on.\n\tAllows listening on an unprivileged port with port forwarding from e.g. 443, and be connected to on port 443.")
	flag.StringVar(&proto, "protocol", "tcp", "Protocol used for listening. 'tcp' for IPv4 and IPv6, 'tcp4' for IPv4, 'tcp6' for IPv6")
	flag.BoolVar(&natEnabled, "nat", false, "Use UPnP/NAT-PMP to acquire external port mapping")
	flag.IntVar(&natLease, "nat-lease", 60, "NAT lease length in minutes")
	flag.IntVar(&natRenewal, "nat-renewal", 30, "NAT renewal frequency in minutes")
	flag.IntVar(&natTimeout, "nat-timeout", 10, "NAT discovery timeout in seconds")
	flag.BoolVar(&pprofEnabled, "pprof", false, "Enable the built in profiling on the status server")
	flag.IntVar(&networkBufferSize, "network-buffer", 2048, "Network buffer size (two of these per proxied connection)")
	flag.Parse()

	if extAddress == "" {
		extAddress = listen
	}

	if len(providedBy) > 30 {
		log.Fatal("Provided-by cannot be longer than 30 characters")
	}

	addr, err := net.ResolveTCPAddr(proto, extAddress)
	if err != nil {
		log.Fatal(err)
	}

	laddr, err := net.ResolveTCPAddr(proto, listen)
	if err != nil {
		log.Fatal(err)
	}
	if laddr.IP != nil && !laddr.IP.IsUnspecified() {
		laddr.Port = 0
		transport, ok := http.DefaultTransport.(*http.Transport)
		if ok {
			transport.Dial = (&net.Dialer{
				Timeout:   30 * time.Second,
				LocalAddr: laddr,
			}).Dial
		}
	}

	log.Println(LongVersion)

	maxDescriptors, err := osutil.MaximizeOpenFileLimit()
	if maxDescriptors > 0 {
		// Assume that 20% of FD's are leaked/unaccounted for.
		descriptorLimit = int64(maxDescriptors*80) / 100
		log.Println("Connection limit", descriptorLimit)

		go monitorLimits()
	} else if err != nil && runtime.GOOS != "windows" {
		log.Println("Assuming no connection limit, due to error retrieving rlimits:", err)
	}

	sessionAddress = addr.IP[:]
	sessionPort = uint16(addr.Port)

	certFile, keyFile := filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		log.Println("Failed to load keypair. Generating one, this might take a while...")
		cert, err = tlsutil.NewCertificate(certFile, keyFile, "strelaysrv", 3072)
		if err != nil {
			log.Fatalln("Failed to generate X509 key pair:", err)
		}
	}

	tlsCfg := &tls.Config{
		Certificates:           []tls.Certificate{cert},
		NextProtos:             []string{protocol.ProtocolName},
		ClientAuth:             tls.RequestClientCert,
		SessionTicketsDisabled: true,
		InsecureSkipVerify:     true,
		MinVersion:             tls.VersionTLS12,
		CipherSuites: []uint16{
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA,
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA,
			tls.TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA,
			tls.TLS_ECDHE_ECDSA_WITH_AES_256_CBC_SHA,
		},
	}

	id := syncthingprotocol.NewDeviceID(cert.Certificate[0])
	if debug {
		log.Println("ID:", id)
	}

	wrapper := config.Wrap("config", config.New(id))
	wrapper.SetOptions(config.OptionsConfiguration{
		NATLeaseM:   natLease,
		NATRenewalM: natRenewal,
		NATTimeoutS: natTimeout,
	})
	natSvc := nat.NewService(id, wrapper)
	mapping := mapping{natSvc.NewMapping(nat.TCP, addr.IP, addr.Port)}

	if natEnabled {
		go natSvc.Serve()
		found := make(chan struct{})
		mapping.OnChanged(func(_ *nat.Mapping, _, _ []nat.Address) {
			select {
			case found <- struct{}{}:
			default:
			}
		})

		// Need to wait a few extra seconds, since NAT library waits exactly natTimeout seconds on all interfaces.
		timeout := time.Duration(natTimeout+2) * time.Second
		log.Printf("Waiting %s to acquire NAT mapping", timeout)

		select {
		case <-found:
			log.Printf("Found NAT mapping: %s", mapping.ExternalAddresses())
		case <-time.After(timeout):
			log.Println("Timeout out waiting for NAT mapping.")
		}
	}

	if sessionLimitBps > 0 {
		sessionLimiter = rate.NewLimiter(rate.Limit(sessionLimitBps), 2*sessionLimitBps)
	}
	if globalLimitBps > 0 {
		globalLimiter = rate.NewLimiter(rate.Limit(globalLimitBps), 2*globalLimitBps)
	}

	if statusAddr != "" {
		go statusService(statusAddr)
	}

	uri, err := url.Parse(fmt.Sprintf("relay://%s/?id=%s&pingInterval=%s&networkTimeout=%s&sessionLimitBps=%d&globalLimitBps=%d&statusAddr=%s&providedBy=%s", mapping.Address(), id, pingInterval, networkTimeout, sessionLimitBps, globalLimitBps, statusAddr, providedBy))
	if err != nil {
		log.Fatalln("Failed to construct URI", err)
	}

	log.Println("URI:", uri.String())

	if poolAddrs == defaultPoolAddrs {
		log.Println("!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!")
		log.Println("!!  Joining default relay pools, this relay will be available for public use. !!")
		log.Println(`!!      Use the -pools="" command line option to make the relay private.      !!`)
		log.Println("!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!")
	}

	pools = strings.Split(poolAddrs, ",")
	for _, pool := range pools {
		pool = strings.TrimSpace(pool)
		if len(pool) > 0 {
			go poolHandler(pool, uri, mapping)
		}
	}

	go listener(proto, listen, tlsCfg)

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	<-sigs

	// Gracefully close all connections, hoping that clients will be faster
	// to realize that the relay is now gone.

	sessionMut.RLock()
	for _, session := range activeSessions {
		session.CloseConns()
	}

	for _, session := range pendingSessions {
		session.CloseConns()
	}
	sessionMut.RUnlock()

	outboxesMut.RLock()
	for _, outbox := range outboxes {
		close(outbox)
	}
	outboxesMut.RUnlock()

	time.Sleep(500 * time.Millisecond)
}

func monitorLimits() {
	limitCheckTimer = time.NewTimer(time.Minute)
	for range limitCheckTimer.C {
		if atomic.LoadInt64(&numConnections)+atomic.LoadInt64(&numProxies) > descriptorLimit {
			atomic.StoreInt32(&overLimit, 1)
			log.Println("Gone past our connection limits. Starting to refuse new/drop idle connections.")
		} else if atomic.CompareAndSwapInt32(&overLimit, 1, 0) {
			log.Println("Dropped below our connection limits. Accepting new connections.")
		}
		limitCheckTimer.Reset(time.Minute)
	}
}

type mapping struct {
	*nat.Mapping
}

func (m *mapping) Address() nat.Address {
	ext := m.ExternalAddresses()
	if len(ext) > 0 {
		return ext[0]
	}
	return m.Mapping.Address()
}
