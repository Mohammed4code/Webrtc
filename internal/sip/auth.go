package sip

import (
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
)

func MD5Hex(s string) string {
	h := md5.Sum([]byte(s))
	return hex.EncodeToString(h[:])
}

func RandomHex(n int) string {
	b := make([]byte, (n+1)/2)
	rand.Read(b)
	return hex.EncodeToString(b)[:n]
}

func CalculateDigest(username, realm, password, nonce, uri, method, qop, cnonce, nc string) string {
	ha1 := MD5Hex(fmt.Sprintf("%s:%s:%s", username, realm, password))
	ha2 := MD5Hex(fmt.Sprintf("%s:%s", method, uri))
	if qop == "auth" {
		return MD5Hex(fmt.Sprintf("%s:%s:%s:%s:%s:%s", ha1, nonce, nc, cnonce, qop, ha2))
	}
	return MD5Hex(fmt.Sprintf("%s:%s:%s", ha1, nonce, ha2))
}

func ParseAuthHeader(header string) (realm, nonce, qop, opaque string) {
	extract := func(key string) string {
		s := key + "=\""
		i := strings.Index(header, s)
		if i == -1 {
			return ""
		}
		i += len(s)
		j := strings.Index(header[i:], "\"")
		if j == -1 {
			return ""
		}
		return header[i : i+j]
	}
	extractRaw := func(key string) string {
		s := key + "="
		i := strings.Index(header, s)
		if i == -1 {
			return ""
		}
		i += len(s)
		j := strings.IndexAny(header[i:], ",\r\n ")
		if j == -1 {
			return strings.Trim(strings.TrimSpace(header[i:]), "\"")
		}
		return strings.Trim(header[i:i+j], "\"")
	}
	realm = extract("realm")
	nonce = extract("nonce")
	opaque = extract("opaque")
	qop = extractRaw("qop")
	if i := strings.Index(qop, ","); i != -1 {
		qop = qop[:i]
	}
	return
}