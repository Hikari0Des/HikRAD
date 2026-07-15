// Command sse measures the FR-31.1 / NFR-1 "packet-to-screen" latency: how
// long after an Accounting-Start is durably ingested until it appears on the
// panel's live SSE feed (GET /api/v1/live/sessions), scripted end to end
// with a headless client instead of a browser. Target: <= 2s.
//
// Requires a running hikrad-acct AND hikrad-api (with a valid manager bearer
// token) reachable from wherever this runs. Not exercised against a live
// stack in the Phase-5 sandbox run (see docs/evidence/README.md); re-run at
// the pilot / in CI on a Linux host.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/hikrad/hikrad/test/perf/perfutil"
)

func main() { os.Exit(run()) }

func run() int {
	sseURL := flag.String("sse-url", "http://127.0.0.1:8080/api/v1/live/sessions", "panel live-sessions SSE endpoint")
	token := flag.String("token", "", "manager bearer token")
	acctAddr := flag.String("acct-addr", "127.0.0.1:8082", "hikrad-acct ingest address")
	nasIP := flag.String("nas-ip", "10.0.0.99", "registered NAS IP to source packets from")
	samples := flag.Int("samples", 10, "number of start-to-visible trials")
	perTrialTimeout := flag.Duration("trial-timeout", 5*time.Second, "max wait per trial before it counts as a miss")
	out := flag.String("out", "../docs/evidence/raw/sse-perf.json", "JSON report path (relative to backend/)")
	flag.Parse()

	if *token == "" {
		fmt.Fprintln(os.Stderr, "-token is required (manager bearer token)")
		return 2
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, *sseURL, nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	req.Header.Set("Authorization", "Bearer "+*token)
	req.Header.Set("Accept", "text/event-stream")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Fprintln(os.Stderr, "connect SSE:", err)
		return 1
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		fmt.Fprintf(os.Stderr, "SSE status %d\n", resp.StatusCode)
		return 1
	}

	lines := make(chan string, 256)
	go func() {
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
		for sc.Scan() {
			select {
			case lines <- sc.Text():
			case <-ctx.Done():
				return
			}
		}
	}()

	var latencies []time.Duration
	var misses int
	base := time.Now().Add(-time.Hour).UTC()
	for i := 0; i < *samples; i++ {
		id := "sse-" + perfutil.RandHex(4) + fmt.Sprint(i)
		rec := perfutil.BuildRecord(*nasIP, id, "sse-user-"+id, "start", base, 0, 0, 0)
		start := time.Now()
		ok, _ := perfutil.PostRecord(*acctAddr, rec)
		if !ok {
			misses++
			continue
		}
		if waitForID(lines, id, *perTrialTimeout) {
			latencies = append(latencies, time.Since(start))
		} else {
			misses++
		}
		// Clean up: close the session so it doesn't linger in live state.
		stop := perfutil.BuildRecord(*nasIP, id, "sse-user-"+id, "stop", base, 5, 0, 0)
		perfutil.PostRecord(*acctAddr, stop)
	}

	pct := perfutil.ComputePercentiles(latencies)
	fmt.Printf("samples=%d misses=%d p50=%s p95=%s p99=%s max=%s\n", *samples, misses, pct.P50, pct.P95, pct.P99, pct.Max)

	rep := map[string]any{
		"samples": *samples, "misses": misses,
		"p50_ms": pct.P50.Seconds() * 1000, "p95_ms": pct.P95.Seconds() * 1000, "p99_ms": pct.P99.Seconds() * 1000,
		"max_ms": pct.Max.Seconds() * 1000, "generated_at": time.Now().UTC(),
	}
	if err := os.MkdirAll(filepath.Dir(*out), 0o755); err == nil {
		if f, err := os.Create(*out); err == nil {
			enc := json.NewEncoder(f)
			enc.SetIndent("", "  ")
			_ = enc.Encode(rep)
			f.Close()
		}
	}

	if misses > 0 || pct.Max > 2*time.Second {
		fmt.Println("[FAIL] NFR-1 gate: packet-to-screen <= 2s for every sample")
		return 1
	}
	fmt.Println("[PASS] NFR-1 gate: packet-to-screen <= 2s")
	return 0
}

// waitForID scans SSE `data:` lines for the session id, up to timeout.
func waitForID(lines <-chan string, id string, timeout time.Duration) bool {
	deadline := time.After(timeout)
	for {
		select {
		case line := <-lines:
			if strings.HasPrefix(line, "data:") && strings.Contains(line, id) {
				return true
			}
		case <-deadline:
			return false
		}
	}
}
