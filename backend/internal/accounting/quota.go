package accounting

// Quota evaluation (contract C8, FR-58.3). On each interim/stop the consumer
// recomputes whether the subscriber has crossed their quota and writes the
// fast-changing flag to quota:exhausted:<subscriber_id>, which D's AuthView
// overlays on every authorize (so a cached view never under-reports exhaustion).
// Enforcement (CoA on crossing) is Phase 3 — this phase only sets the bit.
//
// Quota math EXCLUDES service='hotspot' usage (FR-58.3); graphs/rollups include
// it. The profile quota config is read from the read-only subscriber_quota_view
// that D freezes in C1-D. That view does not exist until D's migrations land, so
// a missing view degrades to "no quota evaluation" and is re-probed periodically
// rather than erroring on the hot path.

import (
	"context"
	"errors"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// quotaKeyPrefix mirrors radius/authview.go's quotaKeyPrefix (contract C8).
const quotaKeyPrefix = "quota:exhausted:"

type quotaEvaluator struct {
	db *pgxpool.Pool
	// viewMissingUntil suppresses re-querying subscriber_quota_view until this
	// unix-nano instant, so a not-yet-created view doesn't hammer the DB during
	// a flood. Re-probed every viewReprobe.
	viewMissingUntil atomic.Int64
}

const viewReprobe = 5 * time.Minute

func newQuotaEvaluator(db *pgxpool.Pool) *quotaEvaluator {
	return &quotaEvaluator{db: db}
}

// quotaConfig is the slice of subscriber_quota_view the evaluator uses. Column
// names follow C1-D's description (subscriber_id, quota_mode, quota bytes, cycle
// anchor).
type quotaConfig struct {
	Mode        string
	TotalBytes  int64
	DownBytes   int64
	UpBytes     int64
	CycleAnchor time.Time
}

// evaluate recomputes the exhausted flag for a subscriber and returns it, or
// (false, false) when there is nothing to evaluate (no subscriber, unlimited,
// or the view is unavailable). The Service writes the Redis key.
func (q *quotaEvaluator) evaluate(ctx context.Context, subscriberID string) (exhausted, evaluated bool) {
	if q.db == nil || subscriberID == "" {
		return false, false
	}
	if until := q.viewMissingUntil.Load(); until != 0 && time.Now().UnixNano() < until {
		return false, false
	}

	cfg, ok, err := q.readConfig(ctx, subscriberID)
	if err != nil {
		if isUndefinedTable(err) {
			q.viewMissingUntil.Store(time.Now().Add(viewReprobe).UnixNano())
		}
		return false, false
	}
	// A successful read means the view exists; clear any suppression.
	q.viewMissingUntil.Store(0)
	if !ok || cfg.Mode == "" || cfg.Mode == "unlimited" {
		return false, true
	}

	var down, up int64
	err = q.db.QueryRow(ctx,
		`SELECT COALESCE(sum(delta_down),0), COALESCE(sum(delta_up),0)
		   FROM usage_points
		  WHERE subscriber_id = $1 AND service <> 'hotspot' AND time >= $2`,
		subscriberID, cfg.CycleAnchor).Scan(&down, &up)
	if err != nil {
		return false, false
	}

	switch cfg.Mode {
	case "total":
		exhausted = cfg.TotalBytes > 0 && down+up >= cfg.TotalBytes
	case "split":
		exhausted = (cfg.DownBytes > 0 && down >= cfg.DownBytes) ||
			(cfg.UpBytes > 0 && up >= cfg.UpBytes)
	}
	return exhausted, true
}

func (q *quotaEvaluator) readConfig(ctx context.Context, subscriberID string) (quotaConfig, bool, error) {
	var c quotaConfig
	var anchor *time.Time
	err := q.db.QueryRow(ctx,
		`SELECT quota_mode,
		        COALESCE(quota_total_bytes,0),
		        COALESCE(quota_down_bytes,0),
		        COALESCE(quota_up_bytes,0),
		        cycle_anchor
		   FROM subscriber_quota_view WHERE subscriber_id = $1`,
		subscriberID).Scan(&c.Mode, &c.TotalBytes, &c.DownBytes, &c.UpBytes, &anchor)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return quotaConfig{}, false, nil
		}
		return quotaConfig{}, false, err
	}
	if anchor != nil {
		c.CycleAnchor = anchor.UTC()
	}
	return c, true, nil
}

func isUndefinedTable(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		// 42P01 undefined_table, 42703 undefined_column — either means the view
		// contract is not in place yet.
		return pgErr.Code == "42P01" || pgErr.Code == "42703"
	}
	return false
}
