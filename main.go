package main

import (
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"webrtc/internal/sip"
	"webrtc/internal/websocket"
)

func serveStatic(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/" {
		http.ServeFile(w, r, filepath.Join("static", "index.html"))
		return
	}
	p := filepath.Join("static", filepath.Clean(strings.TrimPrefix(r.URL.Path, "/")))
	if _, err := os.Stat(p); err == nil {
		http.ServeFile(w, r, p)
		return
	}
	http.ServeFile(w, r, filepath.Join("static", "index.html"))
}

func main() {
	// ربط دوال الإرسال للحزمة sip
	sip.SendToBrowser = func(ext string, msg sip.WSMessage) {
		websocket.SendToBrowser(ext, websocket.WSMessage{
			Type: msg.Type, From: msg.From, SDP: msg.SDP, Status: msg.Status,
		})
	}
	sip.SendStatusToBrowser = websocket.SendStatusToBrowser

	if _, err := os.Stat("static"); os.IsNotExist(err) {
		os.MkdirAll("static", 0755)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", websocket.HandleWebSocket)
	mux.HandleFunc("/", serveStatic)

	log.Println("🚀 SIP-WebRTC bridge on :8080")
	log.Println("📐 Flow: Browser ←SDP/WS→ Go ←SIP/WS→ Asterisk")
	log.Fatal(http.ListenAndServe(":8080", mux))
}