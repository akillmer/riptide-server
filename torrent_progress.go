package main

import (
	socket "github.com/akillmer/go-socket"
	"github.com/anacrolix/torrent"
)

// TorrentProgress stores the latest status of an active torrent
type TorrentProgress struct {
	Hash           string  `json:"hash"`
	BytesCompleted int64   `json:"bytesCompleted"`
	BytesUploaded  int64   `json:"bytesUploaded"`
	BpsUp          int64   `json:"bpsUp"`
	BpsDown        int64   `json:"bpsDown"`
	ActivePeers    int     `json:"activePeers"`
	TotalPeers     int     `json:"totalPeers"`
	Ratio          float64 `json:"ratio"`
}

// Reset the progress to show no activity
func (tp *TorrentProgress) Reset() {
	tp.BpsUp = 0
	tp.BpsDown = 0
	tp.ActivePeers = 0
	tp.TotalPeers = 0
}

// Update a torrent's progress
func (tp *TorrentProgress) Update(t *torrent.Torrent) {
	conn := t.Stats().ConnStats

	// average out this and last BpsUp to be a bit smoother
	tp.BpsUp = (tp.BpsUp + (conn.BytesWritten - tp.BytesUploaded)) / 2
	tp.BytesUploaded = conn.BytesWritten

	tp.BpsDown = (tp.BpsDown + (t.BytesCompleted() - tp.BytesCompleted)) / 2
	tp.BytesCompleted = t.BytesCompleted()

	tp.ActivePeers = t.Stats().ActivePeers
	tp.TotalPeers = t.Stats().TotalPeers

	if tp.BytesCompleted == 0 {
		tp.Ratio = 0
	} else {
		tp.Ratio = float64(tp.BytesUploaded) / float64(tp.BytesCompleted)
	}
}

// Broadcast the torrent's progress
func (tp *TorrentProgress) Broadcast() error {
	return socket.Broadcast(MsgTorrentProgress, tp)
}
