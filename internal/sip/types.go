package sip

import (
	"sync"

	"github.com/gorilla/websocket"
)

type PendingInviteState struct {
	Active    bool
	CallID    string
	Branch    string
	Cseq      int
	ToTag     string
	LocalTag  string
	RemoteTag string
	Target    string
	SDP       string
}

type AsteriskConn struct {
	Conn               *websocket.Conn
	Extension          string
	Password           string
	ServerIP           string
	LocalIP            string
	Registered         bool
	Closed             bool
	CallID             string
	FromTag            string
	Cseq               int
	LastTarget         string
	LastCallID         string
	LastIncomingInvite string
	PendingInvite      PendingInviteState
	Mu                 sync.Mutex
}