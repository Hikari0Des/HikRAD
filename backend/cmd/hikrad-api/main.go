// hikrad-api is the HikRAD REST API server: /api/v1 (public, proxied by
// Caddy) and /internal (service-to-service, unproxied), per Phase-1
// contracts C2–C5. `hikrad-api seed` applies migrations and loads the dev
// fixtures, then exits (the repo-root `make seed` target wraps it).
// `hikrad-api print-tunnel-token` decrypts and prints the configured
// Cloudflare tunnel token for scripts/hikrad's `hikrad tunnel enable` (FR-57).
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/hikrad/hikrad/internal/httpapi"
	"github.com/hikrad/hikrad/internal/platform"
	"github.com/hikrad/hikrad/internal/platform/crypto"
	"github.com/hikrad/hikrad/internal/seed"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(log)

	cfg, err := platform.LoadConfig()
	if err != nil {
		log.Error("configuration error", "error", err)
		os.Exit(1)
	}

	if len(os.Args) > 1 && os.Args[1] == "seed" {
		if err := runSeed(cfg, log); err != nil {
			log.Error("seed failed", "error", err)
			os.Exit(1)
		}
		log.Info("seed complete")
		return
	}

	// print-tunnel-token (FR-57, contract C7): decrypts settings'
	// remote_access.token and writes it to stdout, so scripts/hikrad's
	// `hikrad tunnel enable` can materialize the token file cloudflared reads
	// without reimplementing AES-GCM in bash or ever printing the token to a
	// log. `docker compose exec hikrad-api hikrad-api print-tunnel-token` is
	// the only caller; nothing else needs this on a running install.
	if len(os.Args) > 1 && os.Args[1] == "print-tunnel-token" {
		if err := runPrintTunnelToken(cfg, log); err != nil {
			log.Error("print-tunnel-token failed", "error", err)
			os.Exit(1)
		}
		return
	}

	if err := run(cfg, log); err != nil {
		log.Error("server error", "error", err)
		os.Exit(1)
	}
}

func run(cfg platform.Config, log *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	deps, cleanup, err := startWithRetry(ctx, cfg, log)
	if err != nil {
		return err
	}
	defer cleanup()

	srv := &http.Server{
		Addr:              ":8080",
		Handler:           httpapi.NewRouter(deps, cfg.IsDev()),
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Info("hikrad-api listening", "addr", srv.Addr, "env", cfg.Env)
		errCh <- srv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		log.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			return err
		}
		if err := <-errCh; !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}
}

// startupRetryBudget bounds how long hikrad-api retries its initial
// migration + DB/Redis connection before giving up. Compose's own
// depends_on: condition: service_healthy only watches the container's HTTP
// healthcheck — if the process exits before ever opening :8080, Compose
// treats that as a hard failure and aborts the whole `compose up` rather
// than waiting for `restart: unless-stopped` to try again on a later
// container instance. A cold multi-container start where Postgres/Redis
// report healthy a hair before they actually accept new sessions (found live
// in the Phase-5 M4 install rehearsal: hikrad-api crashed on its very first
// boot, and only a manual `hikrad up` retry succeeded) used to crash
// hikrad-api outright on the first attempt. Retrying inside the process
// instead just makes the container take a little longer to open its port —
// no Compose-level failure, no manual retry needed.
const startupRetryBudget = 90 * time.Second

// startWithRetry runs the migration + dependency-connection sequence,
// retrying with backoff until it succeeds or startupRetryBudget elapses.
func startWithRetry(ctx context.Context, cfg platform.Config, log *slog.Logger) (httpapi.Deps, func(), error) {
	deadline := time.Now().Add(startupRetryBudget)
	backoff := 1 * time.Second
	for {
		deps, cleanup, err := func() (httpapi.Deps, func(), error) {
			if err := platform.Migrate(cfg.DBURL, cfg.MigrationsDir, log); err != nil {
				return httpapi.Deps{}, nil, err
			}
			return buildDeps(ctx, cfg, log)
		}()
		if err == nil {
			return deps, cleanup, nil
		}
		if time.Now().After(deadline) {
			return httpapi.Deps{}, nil, err
		}
		log.Warn("hikrad-api: dependencies not ready yet, retrying startup", "error", err, "retry_in", backoff)
		select {
		case <-ctx.Done():
			return httpapi.Deps{}, nil, ctx.Err()
		case <-time.After(backoff):
		}
		if backoff < 10*time.Second {
			backoff *= 2
		}
	}
}

// buildDeps assembles the frozen C3 Deps struct. Phase 2 (Agent 1) retired the
// Phase-1 auth seams handled here: the internal/auth module now installs the
// real authenticator (httpapi.SetAuthenticator) and mounts POST
// /api/v1/auth/login itself during Register, so buildDeps no longer touches the
// authentication seams — the dev login stub is gone in every environment.
func buildDeps(ctx context.Context, cfg platform.Config, log *slog.Logger) (httpapi.Deps, func(), error) {
	db, err := platform.NewDB(ctx, cfg)
	if err != nil {
		return httpapi.Deps{}, nil, err
	}
	rdb, err := platform.NewRedis(ctx, cfg)
	if err != nil {
		db.Close()
		return httpapi.Deps{}, nil, err
	}
	cleanup := func() {
		_ = rdb.Close()
		db.Close()
	}
	deps := httpapi.Deps{
		DB:       db,
		Redis:    rdb,
		Settings: platform.NewSettings(db),
		Log:      log,
	}
	return deps, cleanup, nil
}

func runSeed(cfg platform.Config, log *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := platform.Migrate(cfg.DBURL, cfg.MigrationsDir, log); err != nil {
		return err
	}
	db, err := platform.NewDB(ctx, cfg)
	if err != nil {
		return err
	}
	defer db.Close()
	return seed.Run(ctx, db, cfg.EncryptionKey)
}

func runPrintTunnelToken(cfg platform.Config, log *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := crypto.Configure(cfg.EncryptionKey); err != nil {
		return err
	}
	db, err := platform.NewDB(ctx, cfg)
	if err != nil {
		return err
	}
	defer db.Close()

	settings := platform.NewSettings(db)
	enc, err := platform.Get[[]byte](ctx, settings, "remote_access.token_enc")
	if err != nil {
		return errors.New("no remote_access.token configured (Settings > Remote Access)")
	}
	if len(enc) == 0 {
		return errors.New("remote_access.token is empty")
	}
	token, err := crypto.Decrypt(enc)
	if err != nil {
		return err
	}
	_, err = os.Stdout.Write(token)
	return err
}
