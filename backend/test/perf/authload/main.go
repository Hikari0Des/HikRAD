// Command authload is the NFR-1 RADIUS auth-latency perf tool: sustains a
// target Access-Request rate against a real FreeRADIUS+hikrad-api and
// reports p50/p95/p99 — the "auth p99 < 100ms at 50 req/s burst over 2k live
// sessions" gate number. It is a self-contained RADIUS client (not an import
// of backend/test/harness, which is package main and B's territory,
// unstaffed this phase) using the same layeh.com/radius library already a
// backend dependency.
//
// Requires a running FreeRADIUS+hikrad-api stack (deploy/compose.yml) with a
// registered NAS matching -nas-ip and a subscriber matching -username — the
// same preconditions as backend/test/harness's load mode. Not exercised
// against a live stack in the Phase-5 sandbox run (documented in
// docs/evidence/README.md); re-run at the pilot / in CI on a Linux host.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hikrad/hikrad/test/perf/perfutil"
	"layeh.com/radius"
	"layeh.com/radius/rfc2865"
)

func main() { os.Exit(run()) }

func run() int {
	addr := flag.String("addr", "127.0.0.1:1812", "FreeRADIUS auth address")
	secret := flag.String("secret", "testing123", "RADIUS shared secret")
	nasIP := flag.String("nas-ip", "10.0.0.99", "NAS-IP-Address to report")
	username := flag.String("username", "testuser", "subscriber username")
	password := flag.String("password", "testpass", "subscriber password")
	rate := flag.Float64("rate", 50, "requests/sec (NFR-1 burst reference: 50)")
	duration := flag.Duration("duration", 30*time.Second, "load duration")
	timeout := flag.Duration("timeout", 5*time.Second, "per-request timeout")
	out := flag.String("out", "../docs/evidence/raw/authload-perf.json", "JSON report path (relative to backend/)")
	flag.Parse()

	interval := time.Duration(float64(time.Second) / *rate)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	var sent, ok, failed int64
	var mu sync.Mutex
	var latencies []time.Duration
	var wg sync.WaitGroup

	deadline := time.Now().Add(*duration)
	for time.Now().Before(deadline) {
		<-ticker.C
		atomic.AddInt64(&sent, 1)
		wg.Add(1)
		go func() {
			defer wg.Done()
			cctx, cancel := context.WithTimeout(context.Background(), *timeout)
			defer cancel()
			start := time.Now()
			accepted, err := papExchange(cctx, *addr, []byte(*secret), *username, *password, *nasIP)
			lat := time.Since(start)
			mu.Lock()
			latencies = append(latencies, lat)
			mu.Unlock()
			if err != nil || !accepted {
				atomic.AddInt64(&failed, 1)
				return
			}
			atomic.AddInt64(&ok, 1)
		}()
	}
	wg.Wait()

	pct := perfutil.ComputePercentiles(latencies)
	fmt.Printf("sent=%d ok=%d failed=%d p50=%s p95=%s p99=%s max=%s\n",
		sent, ok, failed, pct.P50, pct.P95, pct.P99, pct.Max)

	rep := map[string]any{
		"target_rate_pps": *rate, "duration": duration.String(),
		"sent": sent, "ok": ok, "failed": failed,
		"p50_ms": pct.P50.Seconds() * 1000, "p95_ms": pct.P95.Seconds() * 1000, "p99_ms": pct.P99.Seconds() * 1000,
		"generated_at": time.Now().UTC(),
	}
	if err := os.MkdirAll(filepath.Dir(*out), 0o755); err == nil {
		if f, err := os.Create(*out); err == nil {
			enc := json.NewEncoder(f)
			enc.SetIndent("", "  ")
			_ = enc.Encode(rep)
			f.Close()
		}
	}

	if pct.P99 >= 100*time.Millisecond || failed > 0 {
		fmt.Println("[FAIL] NFR-1 gate: p99 < 100ms with zero failures")
		return 1
	}
	fmt.Println("[PASS] NFR-1 gate: p99 < 100ms, zero failures")
	return 0
}

func papExchange(ctx context.Context, addr string, secret []byte, username, password, nasIP string) (accepted bool, err error) {
	p := radius.New(radius.CodeAccessRequest, secret)
	rfc2865.UserName_SetString(p, username)
	rfc2865.UserPassword_SetString(p, password)
	if ip := net.ParseIP(nasIP); ip != nil {
		rfc2865.NASIPAddress_Set(p, ip)
	}
	resp, err := radius.Exchange(ctx, p, addr)
	if err != nil {
		return false, err
	}
	return resp.Code == radius.CodeAccessAccept, nil
}
