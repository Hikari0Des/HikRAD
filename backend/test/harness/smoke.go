package main

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// smokeCase is one assertion the harness makes against a live FreeRADIUS +
// hikrad-api stack (Phase 1 integration gate item 3). Shared between the CLI
// (main.go, default mode) and the go test (smoke_test.go) so both exercise
// exactly the same expectations.
type smokeCase struct {
	name       string
	run        func(ctx context.Context, addr string, secret []byte) result
	wantAccept bool
	// wantRateLimit is checked only when wantAccept is true.
	wantRateLimit string
	// wantReplyContains is checked only when wantAccept is false: the
	// Reply-Message must contain this substring (the C4 reject reason).
	wantReplyContains string
}

func smokeCases(nasIP string) []smokeCase {
	return []smokeCase{
		{
			name: "PAP accept",
			run: func(ctx context.Context, addr string, secret []byte) result {
				return sendPAP(ctx, addr, secret, "testuser", "testpass", nasIP)
			},
			wantAccept:    true,
			wantRateLimit: "10M/10M",
		},
		{
			name: "PAP reject: wrong password",
			run: func(ctx context.Context, addr string, secret []byte) result {
				return sendPAP(ctx, addr, secret, "testuser", "wrongpass", nasIP)
			},
			wantAccept:        false,
			wantReplyContains: "bad_password",
		},
		{
			name: "PAP reject: unknown user",
			run: func(ctx context.Context, addr string, secret []byte) result {
				return sendPAP(ctx, addr, secret, "no-such-user", "whatever", nasIP)
			},
			wantAccept:        false,
			wantReplyContains: "unknown_user",
		},
		{
			name: "CHAP accept",
			run: func(ctx context.Context, addr string, secret []byte) result {
				return sendCHAP(ctx, addr, secret, "testuser", "testpass", nasIP)
			},
			wantAccept:    true,
			wantRateLimit: "10M/10M",
		},
		{
			name: "CHAP reject: wrong password",
			run: func(ctx context.Context, addr string, secret []byte) result {
				return sendCHAP(ctx, addr, secret, "testuser", "wrongpass", nasIP)
			},
			wantAccept:        false,
			wantReplyContains: "bad_password",
		},
	}
}

// runSmoke executes every case against addr/secret and returns the first
// failure description, or "" if every case matched its expectation.
func runSmoke(ctx context.Context, addr string, secret []byte, nasIP string, timeout time.Duration, report func(name string, ok bool, detail string)) (failures int) {
	for _, c := range smokeCases(nasIP) {
		cctx, cancel := context.WithTimeout(ctx, timeout)
		res := c.run(cctx, addr, secret)
		cancel()

		if res.err != nil {
			report(c.name, false, res.err.Error())
			failures++
			continue
		}
		if res.accepted != c.wantAccept {
			report(c.name, false, fmt.Sprintf("accepted=%v, want %v", res.accepted, c.wantAccept))
			failures++
			continue
		}
		if c.wantAccept && res.rateLimit != c.wantRateLimit {
			report(c.name, false, fmt.Sprintf("rate-limit VSA=%q, want %q", res.rateLimit, c.wantRateLimit))
			failures++
			continue
		}
		if !c.wantAccept && !strings.Contains(res.replyMsg, c.wantReplyContains) {
			report(c.name, false, fmt.Sprintf("Reply-Message=%q, want it to contain %q", res.replyMsg, c.wantReplyContains))
			failures++
			continue
		}
		report(c.name, true, fmt.Sprintf("rtt=%s", res.rtt))
	}
	return failures
}
