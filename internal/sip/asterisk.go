package sip

import (
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
	"github.com/gorilla/websocket"
)

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


var SendToBrowser func(extension string, msg WSMessage)


var SendStatusToBrowser func(extension, status string)

func (a *AsteriskConn) StartPingLoop() {
	t := time.NewTicker(20 * time.Second)
	defer t.Stop()
	for range t.C {
		a.Mu.Lock()
		closed := a.Closed
		a.Mu.Unlock()
		if closed {
			return
		}
		go func() {
			a.Mu.Lock()
			defer a.Mu.Unlock()
			if a.Closed || a.Conn == nil {
				return
			}
			branch := "z9hG4bK-" + RandomHex(12)
			msg := fmt.Sprintf(
				"OPTIONS sip:%s SIP/2.0\r\nVia: SIP/2.0/WS %s;branch=%s;rport\r\n"+
					"Max-Forwards: 70\r\nFrom: <sip:%s@%s>;tag=%s\r\nTo: <sip:%s@%s>\r\n"+
					"Call-ID: opt-%s\r\nCSeq: %d OPTIONS\r\nContent-Length: 0\r\n\r\n",
				a.ServerIP, a.LocalIP, branch,
				a.Extension, a.ServerIP, a.FromTag,
				a.Extension, a.ServerIP,
				RandomHex(8), a.Cseq+100,
			)
			a.Conn.WriteMessage(websocket.TextMessage, []byte(msg))
		}()
	}
}


func (a *AsteriskConn) ReceiveMessages() {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("Recovered in ReceiveMessages for %s: %v", a.Extension, r)
		}
	}()

	for {
		a.Mu.Lock()
		if a.Closed || a.Conn == nil {
			a.Mu.Unlock()
			log.Printf("📡 [%s] Stopping ReceiveMessages (connection closed)", a.Extension)
			return
		}
		a.Mu.Unlock()

		_, raw, err := a.Conn.ReadMessage()
		if err != nil {
			
			a.Mu.Lock()
			isClosed := a.Closed
			a.Mu.Unlock()
			if !isClosed {
				log.Printf("❌ [%s] read error: %v", a.Extension, err)
			} else {
				log.Printf("📡 [%s] Connection closed gracefully", a.Extension)
			}
			return
		}
		msg := string(raw)

		// Incoming INVITE
		if strings.HasPrefix(msg, "INVITE ") {
			sdp := ExtractSDP(msg)
			from := ParseFromURI(msg)
			a.Mu.Lock()
			a.LastIncomingInvite = msg
			a.Mu.Unlock()
			a.RespondSIP(msg, "180 Ringing")
			if SendToBrowser != nil {
				SendToBrowser(a.Extension, WSMessage{Type: "incoming", From: from, SDP: sdp})
			}
			log.Printf("📞 [%s] Incoming INVITE from %s", a.Extension, from)
			continue
		}

		// BYE
		if strings.HasPrefix(msg, "BYE ") {
			a.RespondSIP(msg, "200 OK")
			a.Mu.Lock()
			a.PendingInvite.Active = false
			a.Mu.Unlock()
			if SendStatusToBrowser != nil {
				SendStatusToBrowser(a.Extension, "hangup")
			}
			log.Printf("📵 [%s] BYE from remote", a.Extension)
			continue
		}

		// ACK
		if strings.HasPrefix(msg, "ACK ") {
			if SendStatusToBrowser != nil {
				SendStatusToBrowser(a.Extension, "call_answered")
			}
			log.Printf("✅ [%s] ACK received", a.Extension)
			continue
		}

		// 200 OK
		if strings.HasPrefix(msg, "SIP/2.0 200") {
			_, _, _, _, cseq := ParseSIPHeaders(msg)

			if strings.Contains(cseq, "REGISTER") {
				a.Mu.Lock()
				a.Registered = true
				a.Mu.Unlock()
				log.Printf("✅ [%s] REGISTER successful", a.Extension)
				if SendStatusToBrowser != nil {
					SendStatusToBrowser(a.Extension, "registered")
				}
				continue
			}

			if strings.Contains(cseq, "INVITE") {
				sdp := ExtractSDP(msg)
				toTag := ParseToTag(msg)

				a.Mu.Lock()
				if a.PendingInvite.Active {
					a.PendingInvite.ToTag = toTag
				}
				a.Mu.Unlock()

				a.SendAck(toTag)

				if sdp != "" && SendToBrowser != nil {
					SendToBrowser(a.Extension, WSMessage{Type: "answer", SDP: sdp})
				}
				if SendStatusToBrowser != nil {
					SendStatusToBrowser(a.Extension, "call_answered")
				}
				continue
			}
			continue
		}

		// Ringing
		if strings.Contains(msg, "SIP/2.0 180") || strings.Contains(msg, "SIP/2.0 183") {
			if SendStatusToBrowser != nil {
				SendStatusToBrowser(a.Extension, "ringing")
			}
			continue
		}

		// 401/407 Unauthorized
		if strings.Contains(msg, "SIP/2.0 401") || strings.Contains(msg, "SIP/2.0 407") {
			method := "REGISTER"
			if strings.Contains(msg, "INVITE") {
				method = "INVITE"
			}
			if strings.Contains(msg, "OPTIONS") {
				continue
			}

			realm, nonce, qop, opaque := ParseAuthHeader(msg)
			toTag := ParseToTag(msg)
			uri := fmt.Sprintf("sip:%s", a.ServerIP)
			if method == "INVITE" {
				a.Mu.Lock()
				uri = fmt.Sprintf("sip:%s@%s", a.PendingInvite.Target, a.ServerIP)
				a.Mu.Unlock()
			}
			cnonce := RandomHex(8)
			nc := "00000001"
			digest := CalculateDigest(a.Extension, realm, a.Password, nonce, uri, method, qop, cnonce, nc)

			var authLine string
			if qop == "auth" {
				authLine = fmt.Sprintf(
					`Authorization: Digest username="%s", realm="%s", nonce="%s", uri="%s", response="%s", algorithm=MD5, qop=%s, nc=%s, cnonce="%s"`,
					a.Extension, realm, nonce, uri, digest, qop, nc, cnonce)
				if opaque != "" {
					authLine += fmt.Sprintf(`, opaque="%s"`, opaque)
				}
			} else {
				authLine = fmt.Sprintf(
					`Authorization: Digest username="%s", realm="%s", nonce="%s", uri="%s", response="%s", algorithm=MD5`,
					a.Extension, realm, nonce, uri, digest)
			}

			if method == "REGISTER" {
				a.SendRegister(authLine, toTag)
			} else {
				a.SendAck(toTag)
				a.Mu.Lock()
				inv := a.PendingInvite
				newBranch := "z9hG4bK-" + RandomHex(12)
				a.PendingInvite.Branch = newBranch
				a.PendingInvite.Cseq = 2
				a.Mu.Unlock()
				a.SendInvite(inv.Target, inv.SDP, authLine, 2, newBranch, inv.CallID)
			}
			continue
		}

		// Errors
		if strings.Contains(msg, "SIP/2.0 403") || strings.Contains(msg, "SIP/2.0 404") ||
			strings.Contains(msg, "SIP/2.0 486") || strings.Contains(msg, "SIP/2.0 488") {
			if strings.Contains(msg, "REGISTER") {
				if SendStatusToBrowser != nil {
					SendStatusToBrowser(a.Extension, "auth_failed")
				}
			} else {
				if SendStatusToBrowser != nil {
					SendStatusToBrowser(a.Extension, "call_failed")
				}
				a.Mu.Lock()
				a.PendingInvite.Active = false
				a.Mu.Unlock()
			}
			continue
		}

		// 487 Cancelled
		if strings.Contains(msg, "SIP/2.0 487") {
			a.SendAck("")
			a.Mu.Lock()
			a.PendingInvite.Active = false
			a.Mu.Unlock()
			if SendStatusToBrowser != nil {
				SendStatusToBrowser(a.Extension, "hangup")
			}
			continue
		}
	}
}

func ConnectToAsterisk(extension, password, serverIP string) (*AsteriskConn, error) {
	url := fmt.Sprintf("ws://%s:8088/ws", serverIP)
	dialer := websocket.Dialer{HandshakeTimeout: 10 * time.Second, Subprotocols: []string{"sip"}}
	headers := http.Header{}
	headers.Set("Origin", fmt.Sprintf("http://%s", serverIP))
	conn, _, err := dialer.Dial(url, headers)
	if err != nil {
		return nil, err
	}
	localIP := GetLocalIP()
	a := &AsteriskConn{
		Conn:       conn,
		Extension:  extension,
		Password:   password,
		ServerIP:   serverIP,
		LocalIP:    localIP,
		CallID:     RandomHex(16) + "@" + localIP,
		FromTag:    "go-" + RandomHex(12),
		Cseq:       1,
		Registered: false,
		Closed:     false,
	}
	go a.StartPingLoop()
	go a.ReceiveMessages()
	go a.SendRegister("", "")
	log.Printf("🔌 [%s] → Asterisk %s", extension, url)
	return a, nil
}