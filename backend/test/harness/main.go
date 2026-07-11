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
	addr := flag.String("addr", "127.0.0.1:1812", "FreeRADIUS auth address (host:port); in coa-listen mode, the address to bind the mock NAS CoA server")
	secret := flag.String("secret", "testing123", "RADIUS shared secret (must match a clients.conf/NAS entry)")
	nasIP := flag.String("nas-ip", "10.0.0.99", "NAS-IP-Address to report in requests")
	timeout := flag.Duration("timeout", 5*time.Second, "per-request timeout")
	rate := flag.Float64("rate", 0, "load mode: requests/sec to sustain (0 = run the smoke suite once and exit)")
	duration := flag.Duration("duration", 10*time.Second, "duration for -rate load mode and for -mode mndp-announce")
	mode := flag.String("mode", "smoke", "smoke | mndp-announce | coa-listen | enforce | seed-session | voucher-login")
	mndpTarget := flag.String("mndp-target", "255.255.255.255:5678", "mndp-announce: broadcast target host:port")
	mndpIdentity := flag.String("mndp-identity", "HarnessRouter", "mndp-announce: announced identity")
	mndpVersion := flag.String("mndp-version", "7.11", "mndp-announce: announced RouterOS version")
	coaNAK := flag.Bool("coa-nak", false, "coa-listen: reply NAK instead of ACK")
	// enforce mode (FR-9/FR-10 gate item 4): seed a live session + publish an
	// enforce.* event, observe the CoA the worker sends back to this mock NAS.
	redisURL := flag.String("redis", "redis://127.0.0.1:6379/0", "enforce: Redis URL")
	subID := flag.String("subscriber", "", "enforce: subscriber id to enforce")
	username := flag.String("username", "testuser", "enforce/voucher-login: username (or voucher code)")
	nasID := flag.String("nas-id", "", "enforce: NAS id whose coa target is this harness")
	sessionID := flag.String("session-id", "harness-sess-1", "enforce: seeded Acct-Session-Id")
	framedIP := flag.String("framed-ip", "10.10.10.10", "enforce: seeded session IP")
	service := flag.String("service", "pppoe", "enforce/voucher-login: pppoe | hotspot")
	enforceEvent := flag.String("enforce-event", "quota", "enforce: quota | expired")
	observe := flag.Duration("observe", 15*time.Second, "enforce: how long to wait for the CoA")
	flag.Parse()

	switch *mode {
	case "mndp-announce":
		os.Exit(runMNDPAnnounce(*mndpTarget, *mndpIdentity, *mndpVersion, *duration))
	case "coa-listen":
		os.Exit(runCoAListener(*addr, []byte(*secret), *coaNAK))
	case "enforce":
		if *subID == "" || *nasID == "" {
			fmt.Fprintln(os.Stderr, "enforce mode requires -subscriber and -nas-id")
			os.Exit(2)
		}
		os.Exit(runEnforceScenario(enforceOpts{
			coaAddr: *addr, coaSecret: []byte(*secret), redisURL: *redisURL,
			subscriber: *subID, username: *username, nasID: *nasID, sessionID: *sessionID,
			ip: *framedIP, service: *service, event: *enforceEvent, observe: *observe,
		}))
	case "seed-session":
		if *subID == "" || *nasID == "" {
			fmt.Fprintln(os.Stderr, "seed-session mode requires -subscriber and -nas-id")
			os.Exit(2)
		}
		os.Exit(runSeedSession(enforceOpts{
			redisURL: *redisURL, subscriber: *subID, username: *username, nasID: *nasID,
			sessionID: *sessionID, ip: *framedIP, service: *service,
		}))
	case "voucher-login":
		os.Exit(runVoucherLogin(*addr, []byte(*secret), *username, *nasIP, *timeout))
	case "smoke":
		// fall through
	default:
		fmt.Fprintf(os.Stderr, "unknown -mode %q\n", *mode)
		os.Exit(2)
	}

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
