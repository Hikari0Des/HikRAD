package main

// CoA-listener mode: the harness impersonates a NAS's CoA/Disconnect server so
// hikrad-api's CoA service (contract C5) can be exercised end to end and its
// packets asserted. It prints every Disconnect-Request / CoA-Request it
// receives and replies ACK (or NAK when -coa-nak is set) so the caller sees the
// full round trip.

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"layeh.com/radius"
	"layeh.com/radius/rfc2865"
	"layeh.com/radius/rfc2866"
)

// runCoAListener serves a mock NAS CoA endpoint on addr (host:port) with the
// given shared secret, replying ACK unless nak is true.
func runCoAListener(addr string, secret []byte, nak bool) int {
	handler := radius.HandlerFunc(func(w radius.ResponseWriter, r *radius.Request) {
		user := rfc2865.UserName_GetString(r.Packet)
		sess := rfc2866.AcctSessionID_GetString(r.Packet)
		reqName := "CoA-Request"
		ackCode := radius.CodeCoAACK
		nakCode := radius.CodeCoANAK
		if r.Code == radius.CodeDisconnectRequest {
			reqName = "Disconnect-Request"
			ackCode = radius.CodeDisconnectACK
			nakCode = radius.CodeDisconnectNAK
		}
		reply := ackCode
		if nak {
			reply = nakCode
		}
		fmt.Printf("recv %s from %s: user=%q acct_session_id=%q -> %v\n",
			reqName, r.RemoteAddr, user, sess, reply)
		_ = w.Write(r.Packet.Response(reply))
	})

	srv := radius.PacketServer{
		Addr:         addr,
		SecretSource: radius.StaticSecretSource(secret),
		Handler:      handler,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe() }()
	fmt.Printf("CoA listener on %s (reply=%s). Ctrl-C to stop.\n",
		addr, map[bool]string{true: "NAK", false: "ACK"}[nak])

	select {
	case err := <-errCh:
		if err != nil {
			fmt.Printf("listener error: %v\n", err)
			return 1
		}
	case <-ctx.Done():
		_ = srv.Shutdown(context.Background())
	}
	return 0
}
