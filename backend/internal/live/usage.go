package live

// Usage-graph query (contract C7-C / FR-33, Phase-4 task 5 polish). Reads raw
// usage_points directly instead of the usage_daily continuous aggregate:
// usage_daily's "day" bucket is a UTC-midnight boundary (time_bucket with no
// timezone argument), so the last ~3 hours of a Baghdad calendar day/month
// land in the wrong bucket near a day/month edge (Baghdad is UTC+3). Bucketing
// the raw hypertable here with TimescaleDB's timezone-aware time_bucket
// overload fixes that without touching the shared continuous aggregate (still
// used elsewhere — FR-40 etc.); a per-subscriber range scan on the
// (subscriber_id, time) index is cheap enough to do live.
//
// Exported so D's portal usage endpoint (C2: GET /api/v1/portal/usage) can
// call it directly with the token-derived subscriber_id (IDOR rule: no
// subscriber_id ever comes from a URL/query param on the portal API) instead
// of re-querying usage_points itself.

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// UsagePoint is one bucketed usage sample.
type UsagePoint struct {
	T    time.Time `json:"t"`
	Down int64     `json:"down"`
	Up   int64     `json:"up"`
}

// maxUsagePoints caps the response regardless of the requested window
// (response-size cap, task 5): far above any real chart's need, but bounds a
// maliciously/accidentally wide ?from=&to=.
const maxUsagePoints = 800

// UsageForSubscriber returns bucketed usage in Asia/Baghdad calendar
// boundaries. granularity is "daily" or "monthly" (anything else defaults to
// daily). Defense-in-depth only: the caller must already have authorized
// subscriberID for this identity (panel: manager scope; portal: the token's
// own subscriber id) — this function does not check ownership, only shape.
// Never returns a nil slice (a subscriber with no usage yet gets `[]`, not
// null, over the wire).
func UsageForSubscriber(ctx context.Context, db *pgxpool.Pool, subscriberID string, monthly bool, from, to time.Time) ([]UsagePoint, error) {
	bucket := "1 day"
	if monthly {
		bucket = "1 month"
	}
	// Fetch the most recent maxUsagePoints buckets in the window (DESC + LIMIT),
	// then restore chronological order — so an over-wide range is truncated to
	// the newest data rather than the oldest.
	rows, err := db.Query(ctx,
		`SELECT b, down, up FROM (
		   SELECT time_bucket($1::interval, time, 'Asia/Baghdad') AS b,
		          COALESCE(sum(delta_down), 0) AS down,
		          COALESCE(sum(delta_up), 0)   AS up
		     FROM usage_points
		    WHERE subscriber_id = $2::uuid AND time >= $3 AND time < $4
		    GROUP BY b
		    ORDER BY b DESC
		    LIMIT $5
		 ) capped ORDER BY b ASC`,
		bucket, subscriberID, from, to, maxUsagePoints)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]UsagePoint, 0, 64)
	for rows.Next() {
		var p UsagePoint
		if err := rows.Scan(&p.T, &p.Down, &p.Up); err != nil {
			return nil, err
		}
		p.T = p.T.UTC()
		out = append(out, p)
	}
	if rows.Err() != nil {
		return nil, rows.Err()
	}
	return out, nil
}
