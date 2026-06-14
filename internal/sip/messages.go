package sip

import (
	"fmt"
	"strings"
)

func (a *AsteriskConn) Write(raw string) {
	a.Mu.Lock()
	defer a.Mu.Unlock()
	if a.Conn != nil && !a.Closed {
		a.Conn.WriteMessage(1, []byte(raw))
	}
}

func (a *AsteriskConn) SendRegister(authLine, toTag string) {
	a.Mu.Lock()
	branch := "z9hG4bK-" + RandomHex(12)
	callID := a.CallID
	cseq := a.Cseq
	a.Cseq++
	a.Mu.Unlock()

	toHeader := fmt.Sprintf("To: <sip:%s@%s>", a.Extension, a.ServerIP)
	if toTag != "" {
		toHeader += ";tag=" + toTag
	}
	auth := ""
	if authLine != "" {
		auth = authLine + "\r\n"
	}

	a.Write(fmt.Sprintf(
		"REGISTER sip:%s SIP/2.0\r\n"+
			"Via: SIP/2.0/WS %s;branch=%s;rport\r\n"+
			"Max-Forwards: 70\r\n"+
			"From: <sip:%s@%s>;tag=%s\r\n"+
			"%s\r\n"+
			"Call-ID: %s\r\n"+
			"CSeq: %d REGISTER\r\n"+
			"Contact: <sip:%s@%s;transport=ws>\r\n"+
			"Expires: 3600\r\n"+
			"%sContent-Length: 0\r\n\r\n",
		a.ServerIP, a.LocalIP, branch,
		a.Extension, a.ServerIP, a.FromTag,
		toHeader, callID, cseq,
		a.Extension, a.LocalIP, auth,
	))
}

func (a *AsteriskConn) SendInvite(target, sdp, authLine string, cseq int, branch, callID string) {
	toHeader := fmt.Sprintf("To: <sip:%s@%s>", target, a.ServerIP)
	auth := ""
	if authLine != "" {
		auth = authLine + "\r\n"
	}

	a.Write(fmt.Sprintf(
		"INVITE sip:%s@%s SIP/2.0\r\n"+
			"Via: SIP/2.0/WS %s;branch=%s;rport\r\n"+
			"Max-Forwards: 70\r\n"+
			"From: <sip:%s@%s>;tag=%s\r\n"+
			"%s\r\n"+
			"Call-ID: %s\r\n"+
			"CSeq: %d INVITE\r\n"+
			"Contact: <sip:%s@%s;transport=ws>\r\n"+
			"Content-Type: application/sdp\r\n"+
			"%sContent-Length: %d\r\n\r\n%s",
		target, a.ServerIP,
		a.LocalIP, branch,
		a.Extension, a.ServerIP, a.FromTag,
		toHeader, callID, cseq,
		a.Extension, a.LocalIP,
		auth, len(sdp), sdp,
	))
}

func (a *AsteriskConn) SendAck(toTag string) {
	a.Mu.Lock()
	inv := a.PendingInvite
	a.Mu.Unlock()
	if !inv.Active {
		return
	}

	toHeader := fmt.Sprintf("To: <sip:%s@%s>", inv.Target, a.ServerIP)
	if toTag != "" {
		toHeader += ";tag=" + toTag
	}

	a.Write(fmt.Sprintf(
		"ACK sip:%s@%s SIP/2.0\r\n"+
			"Via: SIP/2.0/WS %s;branch=%s;rport\r\n"+
			"Max-Forwards: 70\r\n"+
			"From: <sip:%s@%s>;tag=%s\r\n"+
			"%s\r\n"+
			"Call-ID: %s\r\n"+
			"CSeq: %d ACK\r\n"+
			"Content-Length: 0\r\n\r\n",
		inv.Target, a.ServerIP,
		a.LocalIP, inv.Branch,
		a.Extension, a.ServerIP, a.FromTag,
		toHeader, inv.CallID, inv.Cseq,
	))
}

func (a *AsteriskConn) SendBye() {
	a.Mu.Lock()
	if a.Conn == nil || a.Closed || (!a.PendingInvite.Active && a.LastCallID == "") {
		a.Mu.Unlock()
		return
	}
	callID := a.PendingInvite.CallID
	if callID == "" {
		callID = a.LastCallID
	}
	target := a.PendingInvite.Target
	if target == "" {
		target = a.LastTarget
	}
	localTag := a.PendingInvite.LocalTag
	remoteTag := a.PendingInvite.RemoteTag
	a.Cseq++
	cseq := a.Cseq
	a.PendingInvite.Active = false
	a.Mu.Unlock()

	toHeader := fmt.Sprintf("To: <sip:%s@%s>", target, a.ServerIP)
	if remoteTag != "" {
		toHeader += ";tag=" + remoteTag
	}
	branch := "z9hG4bK-" + RandomHex(12)

	a.Write(fmt.Sprintf(
		"BYE sip:%s@%s SIP/2.0\r\n"+
			"Via: SIP/2.0/WS %s;branch=%s;rport\r\n"+
			"Max-Forwards: 70\r\n"+
			"From: <sip:%s@%s>;tag=%s\r\n"+
			"%s\r\n"+
			"Call-ID: %s\r\n"+
			"CSeq: %d BYE\r\n"+
			"Content-Length: 0\r\n\r\n",
		target, a.ServerIP,
		a.LocalIP, branch,
		a.Extension, a.ServerIP, localTag,
		toHeader, callID, cseq,
	))
}

func (a *AsteriskConn) RespondSIP(raw, status string) {
	via, from, to, callID, cseq := ParseSIPHeaders(raw)
	a.Write(fmt.Sprintf("SIP/2.0 %s\r\n%s\r\n%s\r\n%s\r\n%s\r\n%s\r\nContent-Length: 0\r\n\r\n",
		status, via, from, to, callID, cseq))
}

func (a *AsteriskConn) Respond200SDP(raw, sdp string) {
	via, from, to, callID, cseq := ParseSIPHeaders(raw)

	if !strings.Contains(to, "tag=") {
		to += ";tag=" + RandomHex(8)
	}

	rawCID := strings.TrimSpace(strings.TrimPrefix(callID, "Call-ID:"))
	caller := ParseFromURI(raw)

	a.Mu.Lock()
	a.PendingInvite.Active = true
	a.PendingInvite.CallID = rawCID
	a.PendingInvite.LocalTag = ParseToTag(to + "\r\n")
	a.PendingInvite.RemoteTag = ParseFromTag(raw)

	if caller != "" {
		a.PendingInvite.Target = caller
		a.LastTarget = caller
	}
	a.Mu.Unlock()

	a.Write(fmt.Sprintf(
		"SIP/2.0 200 OK\r\n%s\r\n%s\r\n%s\r\n%s\r\n%s\r\n"+
			"Contact: <sip:%s@%s;transport=ws>\r\n"+
			"Content-Type: application/sdp\r\nContent-Length: %d\r\n\r\n%s",
		via, from, to, callID, cseq,
		a.Extension, a.LocalIP,
		len(sdp), sdp,
	))
}