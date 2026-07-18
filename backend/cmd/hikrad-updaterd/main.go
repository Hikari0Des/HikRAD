// hikrad-updaterd is HikRAD's host-side update daemon (v2 phase 7, FR-86).
// It is deliberately NOT a container image: it must be able to update every
// container in the stack, including any that hosts a caller talking to it,
// so it runs directly on the host, installed by install.sh as a systemd
// unit. It listens on a local unix socket only (bind-mounted into
// hikrad-api's container, never a TCP port), authenticated by a per-install
// shared token, and exposes exactly four verbs — check/update/status/
// rollback-status — as newline-delimited JSON (docs/v2/phases/
// phase-v2-7-one-click-update/00-phase.md, C1/C2).
//
// It does not implement backup, apply, health-checking, or rollback itself
// (FR-86.3): `update` shells into the existing, already-battle-tested
// `hikrad update` CLI path as a child process and relays its progress by
// scanning its output. Because the child's own lock (added to
// scripts/hikrad's cmd_update, C3) is held by the child's own file
// descriptor for the child's own lifetime, "only one update at a time" and
// "rollback survives the daemon dying" (FR-86.5) hold regardless of whether
// this daemon process itself is still running to watch.
package main

import (
	"context"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
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
		log.Error("hikrad-updaterd error", "error", err)
		os.Exit(1)
	}
}

func run(cfg config, log *slog.Logger) error {
	if err := os.MkdirAll(filepath.Dir(cfg.SocketPath), 0o770); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(cfg.LockPath), 0o770); err != nil {
		return err
	}

	// A stale socket file from a prior unclean shutdown makes net.Listen
	// fail with "address already in use" even though nothing is listening.
	_ = os.Remove(cfg.SocketPath)

	ln, err := net.Listen("unix", cfg.SocketPath)
	if err != nil {
		return err
	}
	defer ln.Close()
	_ = os.Chmod(cfg.SocketPath, 0o770)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()

	log.Info("hikrad-updaterd listening", "socket", cfg.SocketPath, "root", cfg.Root)
	srv := newServer(cfg, log)
	return srv.serve(ln)
}
