package sip

import (
	"net"
	"strings"
)

func GetLocalIP() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return "127.0.0.1"
	}
	defer conn.Close()
	return conn.LocalAddr().(*net.UDPAddr).IP.String()
}

func ParseToTag(header string) string {
	i := strings.Index(header, "To:")
	if i == -1 {
		return ""
	}
	j := strings.Index(header[i:], "\r\n")
	if j == -1 {
		return ""
	}
	line := header[i : i+j]
	ti := strings.Index(line, "tag=")
	if ti == -1 {
		return ""
	}
	tag := strings.TrimSpace(line[ti+4:])
	if k := strings.IndexAny(tag, "; \t"); k != -1 {
		tag = tag[:k]
	}
	return tag
}

func ParseFromTag(header string) string {
	i := strings.Index(header, "From:")
	if i == -1 {
		return ""
	}
	j := strings.Index(header[i:], "\r\n")
	if j == -1 {
		return ""
	}
	line := header[i : i+j]
	k := strings.Index(line, "tag=")
	if k == -1 {
		return ""
	}
	tag := line[k+4:]
	if x := strings.IndexAny(tag, "; \t"); x != -1 {
		tag = tag[:x]
	}
	return strings.TrimSpace(tag)
}

func ParseFromURI(header string) string {
	i := strings.Index(header, "From:")
	if i == -1 {
		return ""
	}
	j := strings.Index(header[i:], "\r\n")
	if j == -1 {
		return ""
	}
	line := header[i : i+j]
	si := strings.Index(line, "sip:")
	if si == -1 {
		return ""
	}
	line = line[si+4:]
	k := strings.IndexAny(line, "@>; \t")
	if k == -1 {
		return line
	}
	return line[:k]
}

func ExtractSDP(msg string) string {
	i := strings.Index(msg, "\r\n\r\n")
	if i == -1 {
		return ""
	}
	return msg[i+4:]
}

func ParseSIPHeaders(raw string) (via, from, to, callID, cseq string) {
	for _, line := range strings.Split(raw, "\r\n") {
		switch {
		case strings.HasPrefix(line, "Via:"):
			via = line
		case strings.HasPrefix(line, "From:"):
			from = line
		case strings.HasPrefix(line, "To:"):
			to = line
		case strings.HasPrefix(line, "Call-ID:"):
			callID = line
		case strings.HasPrefix(line, "CSeq:"):
			cseq = line
		}
	}
	return
}