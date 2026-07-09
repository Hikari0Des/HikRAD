package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"sync/atomic"
	"time"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:1812", "FreeRADIUS auth address (host:port)")
	secret := flag.String("secret", "testing123", "RADIUS shared secret (must match a clients.conf entry)")
	nasIP := flag.String("nas-ip", "10.0.0.99", "NAS-IP-Address to report in requests")
	timeout := flag.Duration("timeout", 5*time.Second, "per-request timeout")
	rate := flag.Float64("rate", 0, "load mode: requests/sec to sustain (0 = run the smoke suite once and exit)")
	duration := flag.Duration("duration", 0, "load mode: how long to sustain -rate (required with -rate)")
	flag.Parse()

	if *rate > 0 {
		if *duration <= 0 {
			fmt.Fprintln(os.Stderr, "-duration is required with -rate")
			os.Exit(2)
		}
		os.Exit(runLoad(*addr, []byte(*secret), *nasIP, *timeout, *rate, *duration))
	}
	os.Exit(runSmokeCLI(*addr, []byte(*secret), *nasIP, *timeout))
}

func runSmokeCLI(addr string, secret []byte, nasIP string, timeout time.Duration) int {
	ctx := context.Background()
	failures := runSmoke(ctx, addr, secret, nasIP, timeout, func(name string, ok bool, detail string) {
		status := "PASS"
		if !ok {
			status = "FAIL"
		}
		fmt.Printf("[%s] %s: %s\n", status, name, detail)
	})
	if failures > 0 {
		fmt.Printf("\n%d case(s) failed\n", failures)
		return 1
	}
	fmt.Println("\nall cases passed")
	return 0
}

// runLoad is the NFR-1 perf-verification mode Phase 5 drives: sustain
// -rate accept requests/sec against testuser/testpass for -duration and
// report throughput, error count, and p50/p99-ish latency (min/max as a
// cheap proxy — Phase 5 owns the real percentile tooling).
func runLoad(addr string, secret []byte, nasIP string, timeout time.Duration, rate float64, duration time.Duration) int {
	ctx, cancel := context.WithTimeout(context.Background(), duration+timeout)
	defer cancel()

	interval := time.Duration(float64(time.Second) / rate)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	var sent, ok, failed int64
	var minRTT, maxRTT int64 = int64(time.Hour), 0

	deadline := time.Now().Add(duration)
	for time.Now().Before(deadline) {
		<-ticker.C
		atomic.AddInt64(&sent, 1)
		go func() {
			cctx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()
			res := sendPAP(cctx, addr, secret, "testuser", "testpass", nasIP)
			if res.err != nil || !res.accepted {
				atomic.AddInt64(&failed, 1)
				return
			}
			atomic.AddInt64(&ok, 1)
			rtt := int64(res.rtt)
			for {
				cur := atomic.LoadInt64(&minRTT)
				if rtt >= cur || atomic.CompareAndSwapInt64(&minRTT, cur, rtt) {
					break
				}
			}
			for {
				cur := atomic.LoadInt64(&maxRTT)
				if rtt <= cur || atomic.CompareAndSwapInt64(&maxRTT, cur, rtt) {
					break
				}
			}
		}()
	}
	// Let in-flight requests drain up to `timeout` past the last tick.
	time.Sleep(timeout)

	fmt.Printf("sent=%d ok=%d failed=%d min_rtt=%s max_rtt=%s\n",
		atomic.LoadInt64(&sent), atomic.LoadInt64(&ok), atomic.LoadInt64(&failed),
		time.Duration(atomic.LoadInt64(&minRTT)), time.Duration(atomic.LoadInt64(&maxRTT)))
	if atomic.LoadInt64(&failed) > 0 {
		return 1
	}
	return 0
}
