package main

import (
	"encoding/json"

	socket "github.com/akillmer/go-socket"
	db "github.com/akillmer/riptide/database"
)

// Status describes the current state of a stored torrent
type Status string

// Torrent statuses
const (
	StatusActive  Status = "ACTIVE"
	StatusQueued         = "QUEUED"
	StatusStopped        = "STOPPED"
	StatusPending        = "PENDING"
	StatusDone           = "DONE"
	StatusSeeding        = "SEEDING"
)

// TorrentInfo is static meta information for a particular torrent
type TorrentInfo struct {
	Hash       string `json:"hash"`
	Name       string `json:"name"`
	TimeAdded  int64  `json:"timeAdded"`
	TotalBytes int64  `json:"totalBytes"`
	Status     Status `json:"status"`
	Magnet     string `json:"magnet"`
	LabelID    string `json:"labelID"`
}

// GetTorrentInfo from the database by hash
func GetTorrentInfo(hash string) (*TorrentInfo, error) {
	info := &TorrentInfo{}
	if buf, err := db.Get(db.BucketTorrents, hash); err != nil {
		return nil, err
	} else if err := json.Unmarshal(buf, info); err != nil {
		return nil, err
	}
	return info, nil
}

// GetAllTorrentInfo from the database
func GetAllTorrentInfo() ([]*TorrentInfo, error) {
	buf := db.All(db.BucketTorrents)
	if buf == nil {
		return nil, nil
	}

	all := make([]*TorrentInfo, len(buf))
	for i, b := range buf {
		info := &TorrentInfo{}
		if err := json.Unmarshal(b, info); err != nil {
			return nil, err
		}
		all[i] = info
	}

	return all, nil
}

// SaveAndBroadcast the Torrent info
func (t *TorrentInfo) SaveAndBroadcast() error {
	if err := db.Put(db.BucketTorrents, t.Hash, t); err != nil {
		return err
	}
	socket.Broadcast(MsgTorrentInfo, t)
	return nil
}

// GetLabel assigned to this torrent info from the database
func (t *TorrentInfo) GetLabel() (*Label, error) {
	label := &Label{}
	if buf, err := db.Get(db.BucketLabels, t.LabelID); err != nil {
		return nil, err
	} else if err := json.Unmarshal(buf, label); err != nil {
		return nil, err
	}
	return label, nil
}
