// Command ingest is the NFR-1 accounting-ingest perf tool: sustained
// (~7 pkt/s) and burst (50 pkt/s) load against a real hikrad-acct, sampling
// queue depth (in_queue) to prove it stays near zero in steady state, and
// ack-latency percentiles. See backend/test/chaos/README.md for why this
// drives hikrad-acct's HTTP boundary directly rather than through
// FreeRADIUS.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hikrad/hikrad/test/perf/perfutil"
	"github.com/jackc/pgx/v5/pgxpool"
)

type phaseResult struct {
	Name           string    `json:"name"`
	Rate           float64   `json:"target_rate_pps"`
	Duration       string    `json:"duration"`
	Sent           int64     `json:"sent"`
	Acked          int64     `json:"acked"`
	Failed         int64     `json:"failed"`
	QueueDepthAvg  float64   `json:"queue_depth_avg_steady_state"`
	QueueDepthMax  int64     `json:"queue_depth_max"`
	LatencyP50Ms   float64   `json:"latency_p50_ms"`
	LatencyP95Ms   float64   `json:"latency_p95_ms"`
	LatencyP99Ms   float64   `json:"latency_p99_ms"`
}

type report struct {
	Sessions     int           `json:"sessions"`
	Sustained    phaseResult   `json:"sustained"`
	Burst        phaseResult   `json:"burst"`
	DrainSeconds float64       `json:"post_load_drain_seconds"`
	GeneratedAt  time.Time     `json:"generated_at"`
}

func main() { os.Exit(run()) }

func run() int {
	dbURL := flag.String("db-url", envOr("HIKRAD_TEST_DB_URL", "postgres://hikrad:hikrad@localhost:5432/hikrad_perf?sslmode=disable"), "Postgres URL")
	redisURL := flag.String("redis-url", envOr("HIKRAD_TEST_REDIS_URL", "redis://localhost:6379/3"), "Redis URL")
	migrationsDir := flag.String("migrations", "migrations", "path to backend/migrations (relative to backend/, where this tool is meant to be run from)")
	pgContainer := flag.String("pg-container", "hikrad-perf-postgres", "docker container name")
	redisContainer := flag.String("redis-container", "hikrad-perf-redis", "docker container name")
	provision := flag.Bool("provision", true, "start/stop standalone Postgres+Redis containers")
	acctAddr := flag.String("acct-addr", "127.0.0.1:8094", "address for the managed hikrad-acct")
	sessions := flag.Int("sessions", 200, "concurrent simulated sessions (full mode: 2000)")
	sustainedRate := flag.Float64("sustained-rate", 7, "sustained pkt/s (NFR-1 reference load)")
	sustainedFor := flag.Duration("sustained-for", 30*time.Second, "sustained phase duration (full mode: minutes)")
	burstRate := flag.Float64("burst-rate", 50, "burst pkt/s (NFR-1 reference load)")
	burstFor := flag.Duration("burst-for", 15*time.Second, "burst phase duration")
	out := flag.String("out", "../docs/evidence/raw/ingest-perf.json", "JSON report path (relative to backend/)")
	flag.Parse()

	ctx := context.Background()

	if *provision {
		fmt.Println("== provisioning standalone Postgres+Redis for the perf run ==")
		if err := perfutil.ProvisionPostgres(*pgContainer, *dbURL); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		if err := perfutil.ProvisionRedis(*redisContainer, *redisURL); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		defer func() {
			_ = perfutil.DockerRm(*pgContainer)
			_ = perfutil.DockerRm(*redisContainer)
		}()
	}
	if err := perfutil.WaitTCP(perfutil.HostPort(*dbURL, "5432"), 60*time.Second); err != nil {
		fmt.Fprintln(os.Stderr, "postgres:", err)
		return 1
	}
	if err := perfutil.WaitTCP(perfutil.HostPort(*redisURL, "6379"), 60*time.Second); err != nil {
		fmt.Fprintln(os.Stderr, "redis:", err)
		return 1
	}
	if err := perfutil.Migrate(*dbURL, *migrationsDir); err != nil {
		fmt.Fprintln(os.Stderr, "migrate:", err)
		return 1
	}

	db, err := pgxpool.New(ctx, *dbURL)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer db.Close()

	acct, err := perfutil.BuildAcct(".")
	if err != nil {
		fmt.Fprintln(os.Stderr, "build hikrad-acct:", err)
		return 1
	}
	defer acct.Stop()
	if err := acct.Start(*acctAddr, *dbURL, *redisURL); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	_, nasIP, err := perfutil.ProvisionNAS(ctx, db, "pppoe")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	rep := report{Sessions: *sessions, GeneratedAt: time.Now().UTC()}
	rep.Sustained = runPhase(*acctAddr, "sustained", nasIP, *sessions, *sustainedRate, *sustainedFor)
	fmt.Printf("[sustained] %+v\n", rep.Sustained)
	rep.Burst = runPhase(*acctAddr, "burst", nasIP, *sessions, *burstRate, *burstFor)
	fmt.Printf("[burst]     %+v\n", rep.Burst)

	drainStart := time.Now()
	for {
		s, err := perfutil.FetchCounters(*acctAddr)
		if err == nil && s.InQueue == 0 && s.InvariantOK {
			break
		}
		if time.Since(drainStart) > 2*time.Minute {
			fmt.Fprintln(os.Stderr, "WARNING: queue did not fully drain within 2m")
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	rep.DrainSeconds = time.Since(drainStart).Seconds()

	if err := os.MkdirAll(filepath.Dir(*out), 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	f, err := os.Create(*out)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(rep); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	pass := rep.Sustained.LatencyP99Ms < 100 || rep.Sustained.QueueDepthAvg < 5
	fmt.Printf("\nauth-adjacent note: this tool measures ingest ack latency, not RADIUS auth p99 (see perf/authload). ingest queue depth steady-state: sustained=%.2f burst=%.2f\n",
		rep.Sustained.QueueDepthAvg, rep.Burst.QueueDepthAvg)
	if !pass {
		fmt.Println("[WARN] sustained-phase latency/queue-depth look high; inspect the JSON report")
	}
	return 0
}

func runPhase(acctAddr, name, nasIP string, sessions int, rate float64, duration time.Duration) phaseResult {
	type sess struct {
		acctID, user string
		base         time.Time
		step         int
	}
	pool := make([]*sess, sessions)
	base := time.Now().Add(-time.Hour).UTC()
	for i := range pool {
		id := perfutil.RandHex(4) + fmt.Sprint(i)
		pool[i] = &sess{acctID: name + "-" + id, user: "perf-" + id, base: base}
	}
	steps := []string{"start", "interim", "stop"}

	interval := time.Duration(float64(time.Second) / rate)
	if interval <= 0 {
		interval = time.Millisecond
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	var sent, acked, failed int64
	var latMu sync.Mutex
	var latencies []time.Duration
	var wg sync.WaitGroup

	// Sample queue depth every second in the background.
	stopSampling := make(chan struct{})
	var depths []int64
	go func() {
		t := time.NewTicker(1 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-stopSampling:
				return
			case <-t.C:
				if s, err := perfutil.FetchCounters(acctAddr); err == nil {
					depths = append(depths, s.InQueue)
				}
			}
		}
	}()

	deadline := time.Now().Add(duration)
	idx := 0
	for time.Now().Before(deadline) {
		<-ticker.C
		var s *sess
		for tries := 0; tries < len(pool); tries++ {
			c := pool[idx%len(pool)]
			idx++
			if c.step < len(steps) {
				s = c
				break
			}
		}
		if s == nil {
			break
		}
		typ := steps[s.step]
		secs := s.step * 30
		s.step++
		rec := perfutil.BuildRecord(nasIP, s.acctID, s.user, typ, s.base, secs, uint64(secs)*1000, uint64(secs)*2000)
		atomic.AddInt64(&sent, 1)
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok, lat := perfutil.PostRecord(acctAddr, rec)
			latMu.Lock()
			latencies = append(latencies, lat)
			latMu.Unlock()
			if ok {
				atomic.AddInt64(&acked, 1)
			} else {
				atomic.AddInt64(&failed, 1)
			}
		}()
	}
	wg.Wait()
	close(stopSampling)

	pct := perfutil.ComputePercentiles(latencies)
	var sum int64
	var max int64
	start := 0
	if len(depths) > 2 {
		start = len(depths) / 2 // steady-state: ignore the ramp-up half
	}
	for _, d := range depths[start:] {
		sum += d
		if d > max {
			max = d
		}
	}
	avg := 0.0
	if n := len(depths[start:]); n > 0 {
		avg = float64(sum) / float64(n)
	}

	return phaseResult{
		Name: name, Rate: rate, Duration: duration.String(),
		Sent: sent, Acked: acked, Failed: failed,
		QueueDepthAvg: avg, QueueDepthMax: max,
		LatencyP50Ms: pct.P50.Seconds() * 1000,
		LatencyP95Ms: pct.P95.Seconds() * 1000,
		LatencyP99Ms: pct.P99.Seconds() * 1000,
	}
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
