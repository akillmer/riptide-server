package main

import (
	"errors"
	"log"
	"os"
	"path"
	"sync"
	"time"

	"github.com/anacrolix/torrent/metainfo"

	"github.com/akillmer/riptide/queue"
)

var managedTorrents = sync.Map{}

func addTorrentByMagnet(uri string) error {
	// make sure the client isn't holding this torrent already
	magnet, err := metainfo.ParseMagnetURI(uri)
	if err != nil {
		return err
	}

	if _, ok := client.Torrent(magnet.InfoHash); ok {
		return errors.New("torrent already exists")
	}

	t, err := client.AddMagnet(uri)
	if err != nil {
		return err
	}

	hash := t.InfoHash().String()
	info, err := GetTorrentInfo(hash)
	if err != nil {
		info = &TorrentInfo{
			Hash:      hash,
			TimeAdded: time.Now().Unix(),
			Magnet:    uri,
		}
		info.Status = StatusPending
		info.SaveAndBroadcast()
		<-t.GotInfo()
		info.Name = t.Name()
		info.TotalBytes = t.Length()
	}

	info.Status = StatusQueued
	info.SaveAndBroadcast()

	t.Drop() // this torrent may be going into the queue for a while
	// we need to keep open files to a minimum per torrent client

	return queue.Add(info.Hash)
}

func stopTorrent(hash string) {
	if v, ok := managedTorrents.Load(hash); ok {
		if c, ok := v.(chan struct{}); ok {
			c <- struct{}{}
		}
	}
}

func startTorrent(hash string) {
	closeSignal := make(chan struct{})
	managedTorrents.Store(hash, closeSignal)
	progress := &TorrentProgress{Hash: hash}
	ticker := time.NewTicker(time.Second)

	info, err := GetTorrentInfo(hash)
	if err != nil {
		log.Printf("failed to get torrent info: %v", err)
		return
	}

	if t, err := client.AddMagnet(info.Magnet); err != nil {
		log.Printf("client failed to add magnet: %v", err)
		return
	} else if info.Status == StatusActive {
		if t.Info() == nil {
			<-t.GotInfo()
		}
		t.DownloadAll()
	}

	// whenever the torrent is stopped it's progress activity resets
	defer func() {
		if progress.Hash != "" {
			progress.Reset()
			if err := progress.Broadcast(); err != nil {
				log.Printf("failed to broadcast final progress: %v", err)
			}
		}
	}()

	for {
		select {
		case <-closeSignal:
			goto close
		case <-ticker.C:
			break
		}

		t, ok := client.Torrent(metainfo.NewHashFromHex(hash))
		if !ok {
			log.Printf("client unexpectedly dropped %s", hash)
			break
		} else if t.Info() == nil {
			<-t.GotInfo()
		}

		progress.Update(t)
		progress.Broadcast()

		// grab the latest torrent info from the db, client mightve changed something
		if latest, err := GetTorrentInfo(hash); err != nil {
			log.Printf("failed to get updated info: %v", err)
			break
		} else {
			info = latest
		}

		if info.Status == StatusQueued {
			t.DownloadAll()
			info.Status = StatusActive
		}

		if info.Status == StatusActive {
			if progress.BytesCompleted >= info.TotalBytes {
				info.Status = StatusDone
			}
		}

		if info.Status == StatusDone {
			if label, err := info.GetLabel(); err != nil {
				log.Printf("failed to get label for done torrent: %v", err)
			} else if label.MoveTo != "" {
				if err := os.MkdirAll(label.MoveTo, 0755); err != nil {
					log.Printf("failed to mkdir %s: %v", label.MoveTo, err)
				} else {
					oldPath := path.Join(downloadDir, info.Name)
					newPath := path.Join(label.MoveTo, info.Name)

					if _, err := os.Stat(newPath); err == nil {
						log.Printf("failed moving done data to %s, already exists", newPath)
					} else if err := os.Rename(oldPath, newPath); err != nil {
						log.Printf("failed moving done data: %v", err)
					} else if err := os.Symlink(newPath, oldPath); err != nil {
						log.Printf("failed making symlink to done data: %v", err)
					}
				}
			}

			if globalRatio != -1 && progress.Ratio < globalRatio {
				info.Status = StatusSeeding
			}

			queue.Done(hash)
		}

		if info.Status == StatusSeeding {
			if progress.Ratio >= globalRatio {
				info.Status = StatusDone
			}
		}

		info.SaveAndBroadcast()

		if info.Status == StatusDone {
			break
		}
	}

close:
	if t, ok := client.Torrent(metainfo.NewHashFromHex(hash)); ok {
		t.Drop()
	}
	ticker.Stop()
	managedTorrents.Delete(hash)
	close(closeSignal)
	queue.Done(hash)
}
