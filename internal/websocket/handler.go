package websocket

import (
	"fmt"
	"log"
	"net/http"
	"sync"
	
	"github.com/gorilla/websocket"

	"webrtc/internal/sip"
)

var upgrader = websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

type WSMessage struct {
	Type      string `json:"type"`
	Extension string `json:"extension"`
	Password  string `json:"password"`
	Target    string `json:"target"`
	SDP       string `json:"sdp"`
	Candidate string `json:"candidate"`
	Status    string `json:"status"`
	Digit     string `json:"digit"`
	From      string `json:"from"`
}

var (
	browserConns  = make(map[string]*websocket.Conn)
	asteriskConns = make(map[string]*sip.AsteriskConn)
	browserMu     sync.RWMutex
	asteriskMu    sync.RWMutex
)

func SendToBrowser(extension string, msg WSMessage) {
	browserMu.RLock()
	conn, ok := browserConns[extension]
	browserMu.RUnlock()
	if !ok || conn == nil {
		return
	}
	conn.WriteJSON(msg)
}

func SendStatusToBrowser(extension, status string) {
	SendToBrowser(extension, WSMessage{Type: "status", Status: status})
}

func CloseAsteriskConn(extension string) {
	asteriskMu.Lock()
	defer asteriskMu.Unlock()
	a, ok := asteriskConns[extension]
	if ok && a != nil {
		a.Mu.Lock()
		a.Closed = true
		if a.Conn != nil {
			a.Conn.Close()
			a.Conn = nil
		}
		a.Mu.Unlock()
		delete(asteriskConns, extension)
	}
}

func HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	var ext string

	for {
		var msg WSMessage
		if err := conn.ReadJSON(&msg); err != nil {
			log.Printf("🔌 [%s] browser disconnected", ext)
			if ext != "" {
				CloseAsteriskConn(ext)
				browserMu.Lock()
				delete(browserConns, ext)
				browserMu.Unlock()
			}
			return
		}

		switch msg.Type {
		case "register":
			ext = msg.Extension
			browserMu.Lock()
			browserConns[ext] = conn
			browserMu.Unlock()

			CloseAsteriskConn(ext)
			a, err := sip.ConnectToAsterisk(ext, msg.Password, msg.Target)
			if err != nil {
				conn.WriteJSON(WSMessage{Type: "status", Status: "connection_failed"})
				continue
			}
			asteriskMu.Lock()
			asteriskConns[ext] = a
			asteriskMu.Unlock()

		case "call":
			asteriskMu.RLock()
			ast, ok := asteriskConns[ext]
			asteriskMu.RUnlock()
			if !ok || ast == nil {
				conn.WriteJSON(WSMessage{Type: "status", Status: "error"})
				continue
			}

			target := msg.Target
			browserSDP := msg.SDP
			if browserSDP == "" {
				log.Printf("❌ [%s] call message has no SDP!", ext)
				continue
			}

			log.Printf("📞 [%s] Outgoing call to %s", ext, target)

			ast.Mu.Lock()
			callID := sip.RandomHex(16) + "@" + ast.LocalIP
			branch := "z9hG4bK-" + sip.RandomHex(12)
			ast.PendingInvite = sip.PendingInviteState{
				Active: true,
				CallID: callID,
				Branch: branch,
				Cseq:   1,
				Target: target,
				SDP:    browserSDP,
			}
			ast.LastTarget = target
			ast.LastCallID = callID
			ast.Mu.Unlock()

			ast.SendInvite(target, browserSDP, "", 1, branch, callID)

		case "answer":
			if msg.Target != "caller" {
				continue
			}
			asteriskMu.RLock()
			ast, ok := asteriskConns[ext]
			asteriskMu.RUnlock()
			if !ok || ast == nil {
				continue
			}

			ast.Mu.Lock()
			invite := ast.LastIncomingInvite
			ast.Mu.Unlock()
			if invite == "" {
				log.Printf("❌ [%s] No incoming INVITE stored", ext)
				continue
			}

			log.Printf("✅ [%s] Browser answered incoming call", ext)
			ast.Respond200SDP(invite, msg.SDP)
case "hangup":
	log.Printf("📵 [%s] hangup", ext)
	asteriskMu.RLock()
	ast, ok := asteriskConns[ext]
	asteriskMu.RUnlock()
	if ok && ast != nil {
		ast.SendBye()
		ast.Mu.Lock()
		ast.PendingInvite.Active = false
		ast.PendingInvite = sip.PendingInviteState{}
		ast.Mu.Unlock()
	}
	SendStatusToBrowser(ext, "hangup")
		case "reject":
			asteriskMu.RLock()
			ast, ok := asteriskConns[ext]
			asteriskMu.RUnlock()
			if ok && ast != nil {
				ast.Mu.Lock()
				invite := ast.LastIncomingInvite
				ast.Mu.Unlock()
				if invite != "" {
					ast.RespondSIP(invite, "486 Busy Here")
				}
			}

		case "dtmf":
			asteriskMu.RLock()
			ast, ok := asteriskConns[ext]
			asteriskMu.RUnlock()
			if !ok || ast == nil {
				continue
			}
			ast.Mu.Lock()
			if !ast.Closed && ast.Conn != nil && ast.PendingInvite.Active {
				inv := ast.PendingInvite
				ast.Cseq++
				cseq := ast.Cseq
				ast.Mu.Unlock()
				toHeader := fmt.Sprintf("To: <sip:%s@%s>", inv.Target, ast.ServerIP)
				if inv.ToTag != "" {
					toHeader += ";tag=" + inv.ToTag
				}
				branch := "z9hG4bK-" + sip.RandomHex(12)
				body := fmt.Sprintf("Signal=%s\r\nDuration=250\r\n", msg.Digit)
				ast.Write(fmt.Sprintf(
					"INFO sip:%s@%s SIP/2.0\r\n"+
						"Via: SIP/2.0/WS %s;branch=%s;rport\r\n"+
						"Max-Forwards: 70\r\n"+
						"From: <sip:%s@%s>;tag=%s\r\n"+
						"%s\r\nCall-ID: %s\r\nCSeq: %d INFO\r\n"+
						"Content-Type: application/dtmf-relay\r\nContent-Length: %d\r\n\r\n%s",
					inv.Target, ast.ServerIP,
					ast.LocalIP, branch,
					ast.Extension, ast.ServerIP, ast.FromTag,
					toHeader, inv.CallID, cseq, len(body), body,
				))
				log.Printf("📤 [%s] DTMF %s", ext, msg.Digit)
			} else {
				ast.Mu.Unlock()
			}
		}
	}
}