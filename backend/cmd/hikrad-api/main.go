// hikrad-api is the HikRAD REST API server: /api/v1 (public, proxied by
// Caddy) and /internal (service-to-service, unproxied), per Phase-1
// contracts C2–C5. `hikrad-api seed` applies migrations and loads the dev
// fixtures, then exits (the repo-root `make seed` target wraps it).
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

	if err := run(cfg, log); err != nil {
		log.Error("server error", "error", err)
		os.Exit(1)
	}
}

func run(cfg platform.Config, log *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := platform.Migrate(cfg.DBURL, cfg.MigrationsDir, log); err != nil {
		return err
	}

	deps, cleanup, err := buildDeps(ctx, cfg, log)
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
