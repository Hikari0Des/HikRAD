// Command harness is the MikroTik-simulating RADIUS packet harness (Phase 1,
// Agent 2 / RADIUS & NAS; NFR-8). It proves the real path end to end:
// harness -> FreeRADIUS (UDP 1812) -> hikrad_authorize exec -> hikrad-api's
// /internal/radius/authorize (contract C4) -> back, mapped to
// Mikrotik-Rate-Limit. Phase 2's full policy engine and Phase 5's perf gate
// (-rate/-duration) both build on this same client.
package main

import (
	"context"
	"crypto/md5"
	"crypto/rand"
	"fmt"
	"net"
	"time"

	"layeh.com/radius"
	"layeh.com/radius/rfc2865"
	"layeh.com/radius/vendors/mikrotik"
)

// papRequest builds a PAP Access-Request: User-Password is obfuscated by
// the library per RFC 2865 §5.2 using the packet's own secret+authenticator.
func papRequest(secret []byte, username, password, nasIP string) *radius.Packet {
	p := radius.New(radius.CodeAccessRequest, secret)
	rfc2865.UserName_SetString(p, username)
	rfc2865.UserPassword_SetString(p, password)
	if ip := net.ParseIP(nasIP); ip != nil {
		rfc2865.NASIPAddress_Set(p, ip)
	}
	return p
}

// chapRequest builds a CHAP Access-Request: CHAP-Password is the 17-byte
// RFC 2865 §2.2 value (1-byte ident || MD5(ident || password || challenge)),
// matching backend/internal/radius/stub_policy.go's verifyCHAP exactly.
func chapRequest(secret []byte, username, password, nasIP string) *radius.Packet {
	p := radius.New(radius.CodeAccessRequest, secret)
	rfc2865.UserName_SetString(p, username)
	if ip := net.ParseIP(nasIP); ip != nil {
		rfc2865.NASIPAddress_Set(p, ip)
	}

	challenge := make([]byte, 16)
	if _, err := rand.Read(challenge); err != nil {
		panic(err)
	}
	const ident = 7
	h := md5.New()
	h.Write([]byte{ident})
	h.Write([]byte(password))
	h.Write(challenge)
	chapPassword := append([]byte{ident}, h.Sum(nil)...)

	rfc2865.CHAPChallenge_Set(p, challenge)
	rfc2865.CHAPPassword_Set(p, chapPassword)
	return p
}

// result is the outcome of a single Access-Request, normalized for
// assertions shared between the CLI smoke checks and the go test.
type result struct {
	accepted  bool
	rateLimit string
	replyMsg  string
	rtt       time.Duration
	err       error
}

func send(ctx context.Context, addr string, p *radius.Packet) result {
	start := time.Now()
	resp, err := radius.Exchange(ctx, p, addr)
	rtt := time.Since(start)
	if err != nil {
		return result{err: fmt.Errorf("exchange: %w", err), rtt: rtt}
	}
	return result{
		accepted:  resp.Code == radius.CodeAccessAccept,
		rateLimit: mikrotik.MikrotikRateLimit_GetString(resp),
		replyMsg:  rfc2865.ReplyMessage_GetString(resp),
		rtt:       rtt,
	}
}

func sendPAP(ctx context.Context, addr string, secret []byte, username, password, nasIP string) result {
	return send(ctx, addr, papRequest(secret, username, password, nasIP))
}

func sendCHAP(ctx context.Context, addr string, secret []byte, username, password, nasIP string) result {
	return send(ctx, addr, chapRequest(secret, username, password, nasIP))
}
