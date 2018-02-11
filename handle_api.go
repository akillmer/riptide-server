package main

import (
	"encoding/json"
	"errors"
	"log"
	"os"
	"path"

	socket "github.com/akillmer/go-socket"
	db "github.com/akillmer/riptide/database"
	queue "github.com/akillmer/riptide/queue"
)

// Message types for communicating with the client app
const (
	MsgClientInit      = "CLIENT_INIT"
	MsgClientError     = "CLIENT_ERROR"
	MsgTorrentAdd      = "TORRENT_ADD"
	MsgTorrentStop     = "TORRENT_STOP"
	MsgTorrentInfo     = "TORRENT_INFO"
	MsgTorrentProgress = "TORRENT_PROGRESS"
	MsgTorrentForce    = "TORRENT_FORCE"
	MsgTorrentDelete   = "TORRENT_DELETE"
	MsgTorrentLabelSet = "TORRENT_LABEL_SET"
	MsgLabelUpdate     = "LABEL_UPDATE"
	MsgLabelDelete     = "LABEL_DELETE"
)

// Common errors with the client's use of the API
var (
	ErrBadRequest      = errors.New("bad request")
	ErrTorrentNotFound = errors.New("torrent not found")
)

func sendError(toClient string, err error) {
	log.Printf("%s: %s: %v", MsgClientError, toClient, err)
	if err := socket.Send(toClient, MsgClientError, err.Error()); err != nil {
		log.Printf("failed to send client error: %v", err)
	}
}

func handleAPI() {
	for {
		msg := socket.Read()
		switch msg.Type {

		case MsgTorrentAdd:
			if err := handleMsgTorrentAdd(msg.Payload); err != nil {
				sendError(msg.From, err)
			}

		case MsgTorrentStop:
			if err := handleMsgTorrentStop(msg.Payload); err != nil {
				sendError(msg.From, err)
			}

		case MsgTorrentForce:
			if hash, ok := msg.Payload.(string); ok {
				queue.ForceNext(hash)
			} else {
				sendError(msg.From, ErrBadRequest)
			}

		case MsgTorrentDelete:
			if err := handleMsgTorrentDelete(msg.Payload); err != nil {
				sendError(msg.From, err)
			}

		case MsgTorrentLabelSet:
			if err := handleMsgLabelSet(msg.Payload); err != nil {
				sendError(msg.From, err)
			}

		case MsgLabelUpdate:
			if err := handleMsgLabelUpdate(msg.Payload); err != nil {
				sendError(msg.From, err)
			}

		case MsgLabelDelete:
			if err := handleMsgLabelDelete(msg.Payload); err != nil {
				sendError(msg.From, err)
			}
		}
	}
}

func handleMsgTorrentAdd(payload interface{}) error {
	if uri, ok := payload.(string); ok {
		return addTorrentByMagnet(uri)
	}
	return ErrBadRequest
}

func handleMsgTorrentStop(payload interface{}) error {
	if hash, ok := payload.(string); ok {
		stopTorrent(hash)
		info := &TorrentInfo{}

		if buf, err := db.Get(db.BucketTorrents, hash); buf == nil {
			return ErrTorrentNotFound
		} else if err != nil {
			return err
		} else if err := json.Unmarshal(buf, info); err != nil {
			return err
		}

		info.Status = StatusStopped
		return info.SaveAndBroadcast()
	}
	return ErrBadRequest
}

func handleMsgTorrentDelete(payload interface{}) error {
	if data, ok := payload.(map[string]interface{}); ok {
		var torrentName string

		if hash, ok := data["hash"].(string); ok {
			info, err := GetTorrentInfo(hash)
			if err != nil {
				return err
			}

			torrentName = info.Name
			stopTorrent(hash)

			if err := db.Delete(db.BucketTorrents, hash); err != nil {
				return err
			}
			if err := queue.Remove(hash); err != nil {
				return err
			}
			if err := socket.Broadcast(MsgTorrentDelete, hash); err != nil {
				return err
			}
		} else {
			return ErrBadRequest
		}

		if withData, ok := data["withData"].(bool); ok && withData && torrentName != "" {
			dataFolder := path.Join(downloadDir, torrentName)
			if err := os.RemoveAll(dataFolder); err != nil {
				return err
			}
		}

		return nil
	}
	return ErrBadRequest
}

func handleMsgLabelSet(payload interface{}) error {
	if data, ok := payload.(map[string]interface{}); ok {
		var hash, labelID string
		if h, ok := data["hash"].(string); ok {
			hash = h
		} else {
			return ErrBadRequest
		}

		if id, ok := data["labelID"].(string); ok {
			labelID = id
		} else {
			// a `null` value of labelID means the torrent has no label now
			info, err := GetTorrentInfo(hash)
			if err != nil {
				return err
			}
			info.LabelID = ""
			return info.SaveAndBroadcast()
		}

		// a nil err implies the label exists
		if _, err := GetLabel(labelID); err != nil {
			return err
		}

		info, err := GetTorrentInfo(hash)
		if err != nil {
			return err
		}
		info.LabelID = labelID
		return info.SaveAndBroadcast()
	}
	return ErrBadRequest
}

func handleMsgLabelUpdate(payload interface{}) error {
	if data, ok := payload.(map[string]interface{}); ok {
		if label, err := LabelFromPayload(data); err != nil {
			return err
		} else if err := label.Save(); err != nil {
			return err
		} else {
			return socket.Broadcast(MsgLabelUpdate, label)
		}
	}
	return ErrBadRequest
}

func handleMsgLabelDelete(payload interface{}) error {
	if id, ok := payload.(string); ok {
		if err := DeleteLabel(id); err != nil {
			return err
		}

		if err := socket.Broadcast(MsgLabelDelete, id); err != nil {
			return err
		}

		allInfo, err := GetAllTorrentInfo()
		if err != nil {
			return err
		}

		for _, info := range allInfo {
			if info.LabelID == id {
				info.LabelID = ""
				if err := info.SaveAndBroadcast(); err != nil {
					return err
				}
			}
		}

		return nil
	}
	return ErrBadRequest
}
