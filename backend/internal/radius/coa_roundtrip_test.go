package radius

import (
	"context"
	"net"
	"testing"
	"time"

	"layeh.com/radius"
	"layeh.com/radius/rfc2866"
)

// TestCoARoundTripReal proves a real Disconnect-Request → ACK round trip over
// UDP against a mock NAS built from layeh's server, using the production
// radius.Exchange (Definition of done: "Disconnect ACK round-trip proven").
func TestCoARoundTripReal(t *testing.T) {
	secret := []byte("nassecret")

	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := pc.LocalAddr().String()

	var gotSession string
	srv := radius.PacketServer{
		SecretSource: radius.StaticSecretSource(secret),
		Handler: radius.HandlerFunc(func(w radius.ResponseWriter, r *radius.Request) {
			gotSession = rfc2866.AcctSessionID_GetString(r.Packet)
			_ = w.Write(r.Packet.Response(radius.CodeDisconnectACK))
		}),
	}
	go func() { _ = srv.Serve(pc) }()
	defer srv.Shutdown(context.Background())

	c := &coaService{log: discardLogger(), now: time.Now, exchange: radius.Exchange}
	pkt := radius.New(radius.CodeDisconnectRequest, secret)
	_ = rfc2866.AcctSessionID_SetString(pkt, "sess-123")

	res := c.exchangeWithRetry(context.Background(), pkt, addr, radius.CodeDisconnectRequest)
	if !res.Ok() {
		t.Fatalf("expected ACK round trip, got %+v", res)
	}
	if gotSession != "sess-123" {
		t.Fatalf("NAS saw acct-session-id %q, want sess-123", gotSession)
	}
}

// TestCoARoundTripNAK proves a NAK is surfaced as a typed error the caller can
// fall back on (FR-15.3).
func TestCoARoundTripNAK(t *testing.T) {
	secret := []byte("s")
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := radius.PacketServer{
		SecretSource: radius.StaticSecretSource(secret),
		Handler: radius.HandlerFunc(func(w radius.ResponseWriter, r *radius.Request) {
			_ = w.Write(r.Packet.Response(radius.CodeCoANAK))
		}),
	}
	go func() { _ = srv.Serve(pc) }()
	defer srv.Shutdown(context.Background())

	c := &coaService{log: discardLogger(), now: time.Now, exchange: radius.Exchange}
	res := c.exchangeWithRetry(context.Background(), radius.New(radius.CodeCoARequest, secret),
		pc.LocalAddr().String(), radius.CodeCoARequest)
	if res.Outcome != CoANAK || res.Err == nil {
		t.Fatalf("expected NAK, got %+v", res)
	}
}
