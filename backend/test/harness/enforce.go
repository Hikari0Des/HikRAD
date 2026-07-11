package main

// Enforcement scenario mode (Phase 3, Agent 2): exercises B's runtime
// enforcement worker (contract C4, FR-9/FR-10) against a running stack. It
//   1. binds a mock NAS CoA server on -addr (so the CoA the worker sends lands
//      here — the NAS record for -nas-id must have its coa target pointing at
//      this harness),
//   2. seeds a live session for -subscriber in Redis (livestate, contract C6),
//      so the worker sees an online session to act on, and
//   3. publishes enforce.quota_exceeded / enforce.expired on Redis,
// then observes whether a CoA/Disconnect arrives within -observe. The behavior
// the worker applies comes from D's AuthView for -username, so a real seeded
// subscriber must exist (or the worker will record not_found and send nothing).
//
// This is the automatable half of gate item 4; the human confirms the panel
// audit entry and the ≤ 5 min timing.

import (
	"context"
	"fmt"
	"time"

	"github.com/hikrad/hikrad/internal/live/livestate"
	"github.com/redis/go-redis/v9"
	"layeh.com/radius"
	"layeh.com/radius/rfc2865"
	"layeh.com/radius/rfc2866"
)

type enforceOpts struct {
	coaAddr    string
	coaSecret  []byte
	redisURL   string
	subscriber string
	username   string
	nasID      string
	sessionID  string
	ip         string
	service    string
	event      string // "quota" | "expired"
	observe    time.Duration
}

func runEnforceScenario(o enforceOpts) int {
	channel := "enforce.quota_exceeded"
	if o.event == "expired" {
		channel = "enforce.expired"
	} else if o.event != "quota" {
		fmt.Printf("unknown -enforce-event %q (want quota|expired)\n", o.event)
		return 2
	}

	// 1. Mock NAS CoA server that reports every packet it receives.
	got := make(chan string, 8)
	srv, err := startCoAObserver(o.coaAddr, o.coaSecret, got)
	if err != nil {
		fmt.Printf("bind CoA observer on %s: %v\n", o.coaAddr, err)
		return 1
	}
	defer func() { _ = srv.Shutdown(context.Background()) }()
	fmt.Printf("mock NAS CoA server on %s\n", o.coaAddr)

	// 2. Connect Redis and seed the live session.
	opt, err := redis.ParseURL(o.redisURL)
	if err != nil {
		fmt.Printf("parse -redis: %v\n", err)
		return 2
	}
	rdb := redis.NewClient(opt)
	defer rdb.Close()
	ctx, cancel := context.WithTimeout(context.Background(), o.observe+10*time.Second)
	defer cancel()

	if err := livestate.Upsert(ctx, rdb, livestate.State{
		Username: o.username, SubscriberID: o.subscriber, NASID: o.nasID,
		AcctSessionID: o.sessionID, IP: o.ip, Service: o.service, StartedAt: time.Now(),
	}); err != nil {
		fmt.Printf("seed live session: %v\n", err)
		return 1
	}
	fmt.Printf("seeded live session sub=%s nas=%s sess=%s\n", o.subscriber, o.nasID, o.sessionID)

	// 3. Publish the enforcement event.
	payload := fmt.Sprintf(`{"subscriber_id":%q}`, o.subscriber)
	if err := rdb.Publish(ctx, channel, payload).Err(); err != nil {
		fmt.Printf("publish %s: %v\n", channel, err)
		return 1
	}
	fmt.Printf("published %s %s; observing %s for CoA...\n", channel, payload, o.observe)

	select {
	case line := <-got:
		fmt.Printf("[PASS] observed CoA: %s\n", line)
		return 0
	case <-time.After(o.observe):
		fmt.Printf("[FAIL] no CoA within %s (is hikrad-api up, the NAS coa target this harness, and %s a real online subscriber?)\n", o.observe, o.username)
		return 1
	}
}

// runSeedSession seeds a live session (contract C6) for -subscriber/-username
// on -nas-id, without touching the enforcement channels — the gate-item-1
// counterpart to runEnforceScenario's step 2: it lets a renewal's CoA-restore
// path (which looks up the online session the same way the enforcement worker
// does) be exercised in isolation from FR-9/FR-10's own CoA trigger.
func runSeedSession(o enforceOpts) int {
	opt, err := redis.ParseURL(o.redisURL)
	if err != nil {
		fmt.Printf("parse -redis: %v\n", err)
		return 2
	}
	rdb := redis.NewClient(opt)
	defer rdb.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := livestate.Upsert(ctx, rdb, livestate.State{
		Username: o.username, SubscriberID: o.subscriber, NASID: o.nasID,
		AcctSessionID: o.sessionID, IP: o.ip, Service: o.service, StartedAt: time.Now(),
	}); err != nil {
		fmt.Printf("[FAIL] seed live session: %v\n", err)
		return 1
	}
	fmt.Printf("[PASS] seeded live session sub=%s nas=%s sess=%s\n", o.subscriber, o.nasID, o.sessionID)
	return 0
}

// runVoucherLogin simulates a Hotspot voucher login (FR-18 / AC-18a): it sends
// an Access-Request with the voucher code as both username and password (the
// hotspot login page submits it that way) and reports accept/reject + rate. The
// NAS must be hotspot-typed so the authorize bridge sets service=hotspot and B's
// voucher-detection path runs; a valid unused voucher must exist (owned by D).
func runVoucherLogin(addr string, secret []byte, code, nasIP string, timeout time.Duration) int {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	res := sendPAP(ctx, addr, secret, code, code, nasIP)
	if res.err != nil {
		fmt.Printf("[FAIL] voucher login %q: %v\n", code, res.err)
		return 1
	}
	if !res.accepted {
		fmt.Printf("[FAIL] voucher %q rejected (reply=%q)\n", code, res.replyMsg)
		return 1
	}
	fmt.Printf("[PASS] voucher %q accepted; rate=%q\n", code, res.rateLimit)
	return 0
}

// startCoAObserver binds a CoA/Disconnect server that pushes a description of
// each received packet to got and always ACKs.
func startCoAObserver(addr string, secret []byte, got chan<- string) (*radius.PacketServer, error) {
	handler := radius.HandlerFunc(func(w radius.ResponseWriter, r *radius.Request) {
		name := "CoA-Request"
		ack := radius.CodeCoAACK
		if r.Code == radius.CodeDisconnectRequest {
			name, ack = "Disconnect-Request", radius.CodeDisconnectACK
		}
		desc := fmt.Sprintf("%s user=%q acct_session_id=%q framed_ip=%v",
			name, rfc2865.UserName_GetString(r.Packet), rfc2866.AcctSessionID_GetString(r.Packet),
			rfc2865.FramedIPAddress_Get(r.Packet))
		select {
		case got <- desc:
		default:
		}
		_ = w.Write(r.Packet.Response(ack))
	})
	srv := &radius.PacketServer{
		Addr:         addr,
		SecretSource: radius.StaticSecretSource(secret),
		Handler:      handler,
	}
	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe() }()
	// Give the listener a moment to bind and surface an immediate error.
	select {
	case err := <-errCh:
		return nil, err
	case <-time.After(150 * time.Millisecond):
		return srv, nil
	}
}
