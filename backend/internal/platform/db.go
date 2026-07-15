package platform

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

// NewDB opens the pgx pool used for the C3 Deps.DB and verifies connectivity.
// A failed ping returns an error rather than retrying internally — the
// server-boot caller (cmd/hikrad-api's startWithRetry) is the one that
// retries with backoff; Compose's own restart policy alone isn't enough,
// since a crashed container fails `depends_on: condition: service_healthy`
// outright instead of waiting for a later restart to succeed.
func NewDB(ctx context.Context, cfg Config) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, cfg.DBURL)
	if err != nil {
		return nil, fmt.Errorf("platform: parse HIKRAD_DB_URL: %w", err)
	}
	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("platform: ping postgres: %w", err)
	}
	return pool, nil
}

// NewRedis builds the go-redis client for the C3 Deps.Redis and verifies
// connectivity.
func NewRedis(ctx context.Context, cfg Config) (*redis.Client, error) {
	opts, err := redis.ParseURL(cfg.RedisURL)
	if err != nil {
		return nil, fmt.Errorf("platform: parse HIKRAD_REDIS_URL: %w", err)
	}
	client := redis.NewClient(opts)
	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := client.Ping(pingCtx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("platform: ping redis: %w", err)
	}
	return client, nil
}
