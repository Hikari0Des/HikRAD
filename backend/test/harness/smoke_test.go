package main

// Go integration test per the agent-2 task file ("runnable ... as a Go
// integration test in CI"). Gated on HIKRAD_TEST_RADIUS_ADDR, mirroring the
// HIKRAD_TEST_DB_URL pattern in cmd/hikrad-api/integration_test.go: CI's
// harness-smoke job (.github/workflows/ci.yml) brings up the compose stack
// and sets it; `go test ./...` elsewhere skips.
//
//	HIKRAD_TEST_RADIUS_ADDR=127.0.0.1:1812 go test ./test/harness

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestHarnessSmoke(t *testing.T) {
	addr := os.Getenv("HIKRAD_TEST_RADIUS_ADDR")
	if addr == "" {
		t.Skip("HIKRAD_TEST_RADIUS_ADDR not set; skipping (needs a live FreeRADIUS+hikrad-api stack)")
	}
	secret := os.Getenv("HIKRAD_TEST_RADIUS_SECRET")
	if secret == "" {
		secret = "testing123"
	}

	failures := runSmoke(context.Background(), addr, []byte(secret), "10.0.0.99", 5*time.Second,
		func(name string, ok bool, detail string) {
			if ok {
				t.Logf("PASS %s: %s", name, detail)
			} else {
				t.Errorf("FAIL %s: %s", name, detail)
			}
		})
	if failures > 0 {
		t.Fatalf("%d smoke case(s) failed", failures)
	}
}
