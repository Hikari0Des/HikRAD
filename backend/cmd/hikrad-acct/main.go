// hikrad-acct is HikRAD's lossless accounting ingest service (Phase 2, Agent 3).
// FreeRADIUS forwards every Accounting-Request to POST :8082/acct; the service
// acks 204 only after a durable enqueue (Redis stream + disk spill), then a
// consumer group upserts sessions/usage into TimescaleDB and the live Redis
// state, a reaper closes stale sessions, and /internal/acct/counters proves the
// zero-loss invariant (success metric M2). See internal/accounting.
//
// It does not run migrations (hikrad-api owns the schema) and does not hard-fail
// if Postgres/Redis are momentarily unreachable at boot: ingest keeps accepting
// packets (spilling to disk) and the consumer/reaper retry until the backing
// stores return — that is NFR-2 (accounting never stops) in the wiring.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/hikrad/hikrad/internal/accounting"
	"github.com/hikrad/hikrad/internal/platform"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(log)

	cfg, err := loadConfig()
	if err != nil {
		log.Error("configuration error", "error", err)
		os.Exit(1)
	}
	if err := run(cfg, log); err != nil {
		log.Error("hikrad-acct error", "error", err)
		os.Exit(1)
	}
}

func run(cfg config, log *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// pgxpool.New / redis.NewClient do not dial until first use, so a backing
	// store that is briefly down at boot does not stop the ingest from accepting
	// (and durably spilling) packets.
	db, err := pgxpool.New(ctx, cfg.DBURL)
	if err != nil {
		return err
	}
	defer db.Close()

	ropts, err := redis.ParseURL(cfg.RedisURL)
	if err != nil {
		return err
	}
	rdb := redis.NewClient(ropts)
	defer func() { _ = rdb.Close() }()

	// License boot verification (FR-82.1/82.2, v2 phase 5): every binary
	// independently loads and evaluates the license against this host's
	// fingerprint, same as hikrad-api already does (internal/platform/
	// setupapi.Module). This is informational/defense-in-depth ONLY —
	// RefreshLicenseCache never returns an error this call site could act
	// on, and nothing below may ever branch on license state. FR-50.3's
	// promise (accounting keeps running through and past grace expiry; only
	// hikrad-api's panel-write HTTP gate enforces anything) is unchanged.
	platform.RefreshLicenseCache(ctx, db, log)
	go func() {
		ticker := time.NewTicker(10 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			platform.RefreshLicenseCache(ctx, db, log)
		}
	}()

	svc, cleanup, err := accounting.New(ctx, accounting.Config{
		HTTPAddr:        cfg.Addr,
		SpillDir:        cfg.SpillDir,
		InterimInterval: cfg.Interim,
	}, db, rdb, log)
	if err != nil {
		return err
	}
	defer cleanup()

	return svc.Run(ctx)
}
