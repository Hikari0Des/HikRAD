package notify

// Run is the background half of this package: the outbox dispatcher and the
// day-based scanner, both driven for the process lifetime. Like
// monitorsvc.Run, this is called from hikrad-monitor (the internet-
// dependent background process) — NOT hikrad-api, so Telegram/SMTP/
// WhatsApp/SMS being unreachable can never affect RADIUS auth or
// accounting (NFR-7). The HTTP CRUD surface (module.go) is served by
// hikrad-api, same split monitorsvc already established.

import (
	"context"
	"log/slog"

	"github.com/hikrad/hikrad/internal/platform"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

func Run(ctx context.Context, db *pgxpool.Pool, rdb *redis.Client, settings platform.Settings, log *slog.Logger) {
	d := NewDispatcher(db, rdb, settings, log)
	s := NewScanner(db, rdb, log)
	go d.Run(ctx)
	go s.Run(ctx)
}
