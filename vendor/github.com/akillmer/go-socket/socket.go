package socket

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
	"github.com/teris-io/shortid"
)

// Message that is received from connected clients
type Message struct {
	From    string      `json:"-"`
	Type    string      `json:"type"`
	Payload interface{} `json:"payload"`
}

var (
	mutex      = sync.Mutex{}
	allClients = sync.Map{}
	upgrader   = websocket.Upgrader{}
	closeCodes = []int{1000, 1001, 1002, 1003, 1004, 1005, 1006, 1007,
		1008, 1009, 1010, 1011, 1012, 1013, 1014}
	cMsg chan *Message

	// OnOpen is called whenever a new client is connected
	OnOpen func(clientID string)
	// OnClose is called whenever a client disconnects for any reason
	OnClose func(clientID string)
	// OnError is called whenever an error occurs
	OnError func(clientID string, err error)
	// CheckOrigin is used by Socket when upgrading a WebSocket connection
	CheckOrigin func(r *http.Request) bool
)

func init() {
	cMsg = make(chan *Message)
	upgrader.ReadBufferSize = 1024
	upgrader.WriteBufferSize = 1024
}

// Handler connects a new client. Any errors are sent to the OnError callback or are instead
// sent to standard output.
func Handler(w http.ResponseWriter, r *http.Request) {
	err := func() error {
		upgrader.CheckOrigin = CheckOrigin
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return err
		}

		clientID, err := shortid.Generate()
		if err != nil {
			return err
		}

		allClients.Store(clientID, conn)
		go handleClient(clientID, conn)

		if OnOpen != nil {
			OnOpen(clientID)
		}

		return nil
	}()

	if err != nil {
		if OnError != nil {
			OnError("socket.Handler", err)
		} else {
			log.Printf("socket.Handler error: %v", err)
		}
	}
}

func handleClient(clientID string, conn *websocket.Conn) {
	for {
		msg := &Message{}
		if err := conn.ReadJSON(msg); err != nil {
			if OnError != nil {
				OnError(clientID, err)
			}
			if websocket.IsCloseError(err, closeCodes...) {
				break
			}
		}
		msg.From = clientID
		cMsg <- msg
	}

	if OnClose != nil {
		OnClose(clientID)
	}

	conn.Close()
	allClients.Delete(clientID)
}

// Broadcast to all connected clients
func Broadcast(msgType string, msgPayload interface{}) error {
	buf, err := json.Marshal(&Message{Type: msgType, Payload: msgPayload})
	if err != nil {
		return err
	}

	mutex.Lock()
	defer mutex.Unlock()

	allClients.Range(func(key, val interface{}) bool {
		conn := val.(*websocket.Conn)
		conn.WriteMessage(websocket.TextMessage, buf)
		return true
	})

	return nil
}

// Send a message to a specific client by ID
func Send(clientID, msgType string, msgPayload interface{}) error {
	v, ok := allClients.Load(clientID)
	if !ok {
		return fmt.Errorf("client id %s is not connected", clientID)
	}

	mutex.Lock()
	defer mutex.Unlock()

	msg := &Message{Type: msgType, Payload: msgPayload}
	return v.(*websocket.Conn).WriteJSON(msg)
}

// Read and block for the next available Message
func Read() *Message {
	return <-cMsg
}
