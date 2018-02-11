// Copyright (C) 2015 Audrius Butkevicius and Contributors (see the CONTRIBUTORS file).

//go:generate -command genxdr go run ../../../vendor/github.com/calmh/xdr/cmd/genxdr/main.go
//go:generate genxdr -o packets_xdr.go packets.go

package protocol

import (
	"fmt"
	"net"

	syncthingprotocol "github.com/syncthing/syncthing/lib/protocol"
)

const (
	messageTypePing int32 = iota
	messageTypePong
	messageTypeJoinRelayRequest
	messageTypeJoinSessionRequest
	messageTypeResponse
	messageTypeConnectRequest
	messageTypeSessionInvitation
	messageTypeRelayFull
)

type header struct {
	magic         uint32
	messageType   int32
	messageLength int32
}

type Ping struct{}
type Pong struct{}
type JoinRelayRequest struct{}
type RelayFull struct{}

type JoinSessionRequest struct {
	Key []byte // max:32
}

type Response struct {
	Code    int32
	Message string
}

type ConnectRequest struct {
	ID []byte // max:32
}

type SessionInvitation struct {
	From         []byte // max:32
	Key          []byte // max:32
	Address      []byte // max:32
	Port         uint16
	ServerSocket bool
}

func (i SessionInvitation) String() string {
	return fmt.Sprintf("%s@%s", syncthingprotocol.DeviceIDFromBytes(i.From), i.AddressString())
}

func (i SessionInvitation) GoString() string {
	return i.String()
}

func (i SessionInvitation) AddressString() string {
	return fmt.Sprintf("%s:%d", net.IP(i.Address), i.Port)
}
