// Command chaos is Phase 5's scripted, reproducible chaos-evidence tool for
// the lossless accounting pipeline (FR-37.5, NFR-2; sub-PRD 03 §7). It drives
// a real hikrad-acct process (built fresh from backend/cmd/hikrad-acct) with
// real Postgres/Redis backing stores, injects the failures named in the
// phase brief, and proves the FR-40 counter invariant
// (received - deduplicated - in_queue == persisted) survives each one with
// zero record loss. See README.md for the full scenario list and how this
// differs from internal/accounting's code-level chaos_test.go suite.
//
// Every scenario writes a JSON result to -out (default docs/evidence/raw/)
// so docs/evidence/generate.sh can fold it into the dated evidence report.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

func main() {
	os.Exit(run())
}

func run() int {
	scenario := flag.String("scenario", "", "kill-postgres | kill-redis | kill-acct | unclean-reboot | retransmit-storm | out-of-order | panel-down | spill-corruption | redis-durability | all")
	dbURL := flag.String("db-url", envOr("HIKRAD_TEST_DB_URL", "postgres://hikrad:hikrad@localhost:5432/hikrad_chaos?sslmode=disable"), "Postgres URL for the chaos DB")
	redisURL := flag.String("redis-url", envOr("HIKRAD_TEST_REDIS_URL", "redis://localhost:6379/2"), "Redis URL for the chaos run")
	acctAddr := flag.String("acct-addr", "127.0.0.1:8093", "address the managed hikrad-acct child process listens on")
	migrationsDir := flag.String("migrations", "migrations", "path to backend/migrations (relative to backend/, where this tool is meant to be run from)")
	pgContainer := flag.String("postgres-container", "hikrad-chaos-postgres", "docker container name for the chaos Postgres (managed by -provision)")
	redisContainer := flag.String("redis-container", "hikrad-chaos-redis", "docker container name for the chaos Redis (managed by -provision)")
	provision := flag.Bool("provision", true, "start/stop standalone Postgres+Redis docker containers for the run (set false to reuse an already-running pair)")
	redisFsync := flag.String("redis-fsync", "everysec", "everysec (deploy/compose.yml's current default) | always (docs/evidence/redis-durability-decision.md's recommendation) — only used with -provision")
	sessions := flag.Int("sessions", 200, "concurrent simulated sessions (CI-nightly default 200; full mode targets 2000)")
	rate := flag.Float64("rate", 50, "aggregate accounting packets/sec")
	duration := flag.Duration("duration", 20*time.Second, "flood duration")
	killFor := flag.Duration("kill-for", 15*time.Second, "how long the target dependency stays down")
	interims := flag.Int("interims", 2, "interim records per simulated session")
	outDir := flag.String("out", "../docs/evidence/raw", "directory for JSON scenario results (relative to backend/)")
	verbose := flag.Bool("v", false, "verbose logging")
	flag.Parse()

	if *scenario == "" {
		fmt.Fprintln(os.Stderr, "usage: chaos -scenario <name> [flags]  (see README.md)")
		return 2
	}
	if !*verbose {
		log.SetOutput(os.Stderr)
	}

	rig := &Rig{
		DBURL:          *dbURL,
		RedisURL:       *redisURL,
		AcctAddr:       *acctAddr,
		MigrationsDir:  *migrationsDir,
		PGContainer:    *pgContainer,
		RedisContainer: *redisContainer,
		Sessions:       *sessions,
		Rate:           *rate,
		Duration:       *duration,
		KillFor:        *killFor,
		Interims:       *interims,
	}

	ctx := context.Background()

	if *provision {
		fmt.Println("== provisioning standalone Postgres+Redis for the chaos run ==")
		if err := provisionPostgres(*pgContainer, *dbURL); err != nil {
			fmt.Fprintln(os.Stderr, "provision postgres:", err)
			return 1
		}
		if err := provisionRedis(*redisContainer, *redisURL, *redisFsync); err != nil {
			fmt.Fprintln(os.Stderr, "provision redis:", err)
			return 1
		}
		defer func() {
			_ = dockerRm(*pgContainer)
			_ = dockerRm(*redisContainer)
		}()
	}

	if err := waitTCP(pgHostPort(*dbURL), 60*time.Second); err != nil {
		fmt.Fprintln(os.Stderr, "postgres not reachable:", err)
		return 1
	}
	if err := waitTCP(redisHostPort(*redisURL), 60*time.Second); err != nil {
		fmt.Fprintln(os.Stderr, "redis not reachable:", err)
		return 1
	}

	if err := migrateForChaos(*dbURL, *migrationsDir); err != nil {
		fmt.Fprintln(os.Stderr, "migrate:", err)
		return 1
	}

	db, err := pgxpool.New(ctx, *dbURL)
	if err != nil {
		fmt.Fprintln(os.Stderr, "db pool:", err)
		return 1
	}
	defer db.Close()
	ropts, err := redis.ParseURL(*redisURL)
	if err != nil {
		fmt.Fprintln(os.Stderr, "redis url:", err)
		return 1
	}
	rdb := redis.NewClient(ropts)
	defer rdb.Close()
	rig.db = db
	rig.rdb = rdb

	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "mkdir out:", err)
		return 1
	}
	rig.OutDir = *outDir

	if err := rig.buildAcctBinary(); err != nil {
		fmt.Fprintln(os.Stderr, "build hikrad-acct:", err)
		return 1
	}
	defer rig.cleanupBinary()

	names := strings.Split(*scenario, ",")
	if *scenario == "all" {
		names = []string{"kill-postgres", "kill-redis", "kill-acct", "unclean-reboot", "retransmit-storm", "out-of-order", "panel-down", "spill-corruption", "redis-durability"}
	}

	failed := 0
	for i, name := range names {
		name = strings.TrimSpace(name)
		if i > 0 {
			if err := rig.resetForNextScenario(ctx); err != nil {
				fmt.Fprintln(os.Stderr, "reset between scenarios:", err)
				return 1
			}
		}
		res, err := rig.runScenario(ctx, name)
		if err != nil {
			fmt.Printf("[FAIL] %-20s error: %v\n", name, err)
			failed++
			continue
		}
		status := "PASS"
		if !res.Pass {
			status = "FAIL"
			failed++
		}
		fmt.Printf("[%s] %-20s %s\n", status, name, res.Detail)
		if err := writeReport(filepath.Join(*outDir, name+".json"), res); err != nil {
			fmt.Fprintln(os.Stderr, "write report:", err)
		}
	}

	if failed > 0 {
		fmt.Printf("\n%d scenario(s) failed\n", failed)
		return 1
	}
	fmt.Println("\nall chaos scenarios passed: FR-40 invariant held, zero records lost")
	return 0
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
