package cards

// Live-derived per-card usage since usage_cycle_anchor — the same "derive,
// never store-and-edit" posture subscribers/limits.go's sumUptimeSeconds/
// sumDailyBytes already use, keyed by the card_id column migration 0604
// added to sessions/usage_points instead of subscriber_id.

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func sumCardBytes(ctx context.Context, db *pgxpool.Pool, cardID string, since time.Time) (down, up int64, err error) {
	err = db.QueryRow(ctx,
		`SELECT COALESCE(SUM(delta_down), 0), COALESCE(SUM(delta_up), 0)
		   FROM usage_points WHERE card_id = $1::uuid AND time >= $2::timestamptz`,
		cardID, since).Scan(&down, &up)
	return
}

func sumCardUptime(ctx context.Context, db *pgxpool.Pool, cardID string, since time.Time) (int64, error) {
	var seconds float64
	err := db.QueryRow(ctx,
		`SELECT COALESCE(SUM(EXTRACT(EPOCH FROM (COALESCE(stopped_at, now()) - GREATEST(started_at, $2::timestamptz)))), 0)
		   FROM sessions
		  WHERE card_id = $1::uuid AND started_at IS NOT NULL
		    AND COALESCE(stopped_at, now()) > $2::timestamptz`,
		cardID, since).Scan(&seconds)
	if err != nil {
		return 0, err
	}
	if seconds < 0 {
		seconds = 0
	}
	return int64(seconds), nil
}
