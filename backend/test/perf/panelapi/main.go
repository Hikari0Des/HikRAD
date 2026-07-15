// Command panelapi is the NFR-1 "panel pages load < 1.5s" perf tool: fires
// repeated GETs at the heavy panel endpoints (dashboard, subscriber list,
// usage graphs) with a real bearer token and reports p95 per endpoint.
//
// Requires a running hikrad-api with a valid manager bearer token. Not
// exercised against a live stack in the Phase-5 sandbox run (see
// docs/evidence/README.md); re-run at the pilot / in CI on a Linux host.
package main

import (
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
	baseURL := flag.String("base-url", "http://127.0.0.1:8080", "hikrad-api base URL")
	token := flag.String("token", "", "manager bearer token")
	endpoints := flag.String("endpoints", strings.Join([]string{
		"/api/v1/dashboard",
		"/api/v1/subscribers?limit=50",
		"/api/v1/usage/network?granularity=day",
	}, ","), "comma-separated endpoint paths to load-check")
	requests := flag.Int("requests", 20, "requests per endpoint")
	out := flag.String("out", "../docs/evidence/raw/panelapi-perf.json", "JSON report path (relative to backend/)")
	flag.Parse()

	if *token == "" {
		fmt.Fprintln(os.Stderr, "-token is required (manager bearer token)")
		return 2
	}

	type row struct {
		Endpoint         string  `json:"endpoint"`
		N                int     `json:"n"`
		Failed           int     `json:"failed"`
		P50Ms, P95Ms, P99Ms float64 `json:"-"`
	}
	results := make(map[string]any)
	failGate := false

	for _, ep := range strings.Split(*endpoints, ",") {
		ep = strings.TrimSpace(ep)
		if ep == "" {
			continue
		}
		var latencies []time.Duration
		failed := 0
		for i := 0; i < *requests; i++ {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			req, _ := http.NewRequestWithContext(ctx, http.MethodGet, *baseURL+ep, nil)
			req.Header.Set("Authorization", "Bearer "+*token)
			start := time.Now()
			resp, err := http.DefaultClient.Do(req)
			lat := time.Since(start)
			cancel()
			if err != nil || resp.StatusCode >= 400 {
				failed++
				if resp != nil {
					resp.Body.Close()
				}
				continue
			}
			resp.Body.Close()
			latencies = append(latencies, lat)
		}
		pct := perfutil.ComputePercentiles(latencies)
		fmt.Printf("%-40s n=%d failed=%d p50=%s p95=%s p99=%s\n", ep, *requests, failed, pct.P50, pct.P95, pct.P99)
		results[ep] = map[string]any{
			"n": *requests, "failed": failed,
			"p50_ms": pct.P50.Seconds() * 1000, "p95_ms": pct.P95.Seconds() * 1000, "p99_ms": pct.P99.Seconds() * 1000,
		}
		if failed > 0 || pct.P95 >= 1500*time.Millisecond {
			failGate = true
		}
	}

	if err := os.MkdirAll(filepath.Dir(*out), 0o755); err == nil {
		if f, err := os.Create(*out); err == nil {
			enc := json.NewEncoder(f)
			enc.SetIndent("", "  ")
			_ = enc.Encode(map[string]any{"endpoints": results, "generated_at": time.Now().UTC()})
			f.Close()
		}
	}

	if failGate {
		fmt.Println("[FAIL] NFR-1 gate: panel API p95 < 1.5s on every endpoint, zero failures")
		return 1
	}
	fmt.Println("[PASS] NFR-1 gate: panel API p95 < 1.5s")
	return 0
}
