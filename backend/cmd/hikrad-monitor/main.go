// hikrad-monitor is HikRAD's monitoring service (Phase 3, Agent 3): it runs the
// ICMP/SNMP probe engine + up/down state machine (FR-34/FR-60), the alerts
// engine (FR-36, in-app/Telegram/SMTP/WhatsApp with quiet-hours + cooldown), the
// system self-checks (FR-35/FR-40 surfacing), and the dashboard sampler (FR-32).
// The read/CRUD HTTP surface for all of it is served by hikrad-api (the
// internal/monitorsvc Module); this binary owns only the background loops.
//
// Like hikrad-acct it does not run migrations (hikrad-api owns the schema) and
// does not hard-fail if Postgres/Redis are momentarily unreachable at boot — the
// loops retry until the backing stores return.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/hikrad/hikrad/internal/monitorsvc"
	"github.com/hikrad/hikrad/internal/platform"
	"github.com/hikrad/hikrad/internal/platform/crypto"
	"github.com/hikrad/hikrad/internal/push"
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
		log.Error("hikrad-monitor error", "error", err)
		os.Exit(1)
	}
}

func run(cfg config, log *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// The encryption key lets the probe engine decrypt SNMP communities (NAS +
	// device). It's optional: without it, targets simply probe ICMP-only.
	if cfg.EncryptionKey != nil {
		if err := crypto.Configure(cfg.EncryptionKey); err != nil {
			log.Warn("hikrad-monitor: bad encryption key, SNMP disabled", "error", err)
		}
	}

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
	// promise (probes/alerts keep running through and past grace expiry;
	// only hikrad-api's panel-write HTTP gate enforces anything) is unchanged.
	platform.RefreshLicenseCache(ctx, db, log)
	go func() {
		ticker := time.NewTicker(10 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			platform.RefreshLicenseCache(ctx, db, log)
		}
	}()

	settings := platform.NewSettings(db)
	// push.Module.Register never runs in this process (it's an httpapi hook,
	// and hikrad-monitor doesn't mount HTTP modules) — the alert engine's push
	// channel needs its own explicit wiring here (contract C4, see the
	// package doc comment on why this is required in both binaries).
	push.Init(db, rdb, settings, log)
	return monitorsvc.Run(ctx, db, rdb, settings, log)
}
