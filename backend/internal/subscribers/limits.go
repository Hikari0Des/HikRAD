package subscribers

// v4-4 Limits engine loader helpers (FR-128–131). GetAuthView (authview.go)
// calls these to populate radius.AuthView's new fields. Kept in a separate
// file so the already-large authview.go query doesn't grow a second screen
// of unrelated helper functions.

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// speedWindowRow is the JSON shape GetAuthView's jsonb_agg produces for one
// profile_speed_windows row (FR-131).
type speedWindowRow struct {
	ID           string `json:"id"`
	DaysMask     int    `json:"days_mask"`
	StartMin     int    `json:"start_min"`
	EndMin       int    `json:"end_min"`
	RateDownKbps int    `json:"rate_down_kbps"`
	RateUpKbps   int    `json:"rate_up_kbps"`
}

// resolveExhaustTarget interprets one limit's exhaust_target column by its
// action (FR-130): a rate string for throttle, a pool-name override for
// expired_pool, or a target profile id to resolve for switch_plan. Only
// switch_plan needs a DB round trip (resolveSwitchTargetProfile); the other
// two actions are a straight passthrough of the already-loaded text.
func resolveExhaustTarget(ctx context.Context, db *pgxpool.Pool, action, target string) (throttleRate, poolOverride, switchRate, switchPool string, err error) {
	switch action {
	case "throttle":
		throttleRate = target
	case "expired_pool":
		poolOverride = target
	case "switch_plan":
		if target == "" || !looksLikeUUID(target) {
			return
		}
		switchRate, switchPool, err = resolveSwitchTargetProfile(ctx, db, target)
	}
	return
}

// resolveSwitchTargetProfile resolves a switch_plan target profile id into
// its own rate + pool — a switch is "become this whole other plan", so the
// RADIUS engine (which never touches the DB) needs the target's attributes
// already resolved, not the raw id. A missing/deleted target profile
// degrades to "no override" rather than failing the whole auth: an operator
// deleting a profile that is still referenced as a switch target is a config
// mistake to fix, not a reason to lock every affected subscriber out.
func resolveSwitchTargetProfile(ctx context.Context, db *pgxpool.Pool, targetProfileID string) (rate, pool string, err error) {
	var rateDown, rateUp *int
	var poolName *string
	err = db.QueryRow(ctx,
		`SELECT tp.rate_down_kbps, tp.rate_up_kbps, (SELECT name FROM ip_pools WHERE id = tp.pool_id)
		   FROM profiles tp WHERE tp.id = $1::uuid`, targetProfileID).Scan(&rateDown, &rateUp, &poolName)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", nil
	}
	if err != nil {
		return "", "", err
	}
	return rateString(intOr(rateUp, 0), intOr(rateDown, 0)), strOr(poolName, ""), nil
}

// looksLikeUUID guards the switch_plan target cast: exhaust_target is free
// text (it holds a rate string or a pool name for the other two actions), so
// blindly casting it to uuid would error on anything that isn't one. A saved
// switch_plan target is always a real uuid (profiles/api.go validates it at
// write time); this only protects a raw DB edit or pre-validation row.
func looksLikeUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, r := range s {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if r != '-' {
				return false
			}
			continue
		}
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
			return false
		}
	}
	return true
}

// ensureCounters loads (creating on first touch) the subscriber's resettable
// FR-128/FR-129 cycle anchors. A missing row means this subscriber has never
// been evaluated against a limit before — it is created with both anchors at
// now(), so uptime/daily accrual starts from this moment rather than
// retroactively counting a subscriber's entire history the first time an
// operator turns a limit on.
func ensureCounters(ctx context.Context, db *pgxpool.Pool, subscriberID string) (uptimeAnchor, dailyStart time.Time, err error) {
	err = db.QueryRow(ctx,
		`INSERT INTO subscriber_counters (subscriber_id) VALUES ($1::uuid)
		 ON CONFLICT (subscriber_id) DO UPDATE SET subscriber_id = EXCLUDED.subscriber_id
		 RETURNING uptime_cycle_anchor, daily_period_start`, subscriberID).Scan(&uptimeAnchor, &dailyStart)
	return
}

// sumUptimeSeconds derives cumulative connected time since anchor — never
// stored-and-edited, computed live on every load exactly like quota
// (accounting/quota.go sums usage_points the same way). An open session
// (stopped_at IS NULL) counts up to now(); a session that started before the
// anchor counts only from the anchor forward.
func sumUptimeSeconds(ctx context.Context, db *pgxpool.Pool, subscriberID string, anchor time.Time) (int64, error) {
	var seconds float64
	err := db.QueryRow(ctx,
		`SELECT COALESCE(SUM(EXTRACT(EPOCH FROM (COALESCE(stopped_at, now()) - GREATEST(started_at, $2::timestamptz)))), 0)
		   FROM sessions
		  WHERE subscriber_id = $1::uuid AND started_at IS NOT NULL
		    AND COALESCE(stopped_at, now()) > $2::timestamptz`,
		subscriberID, anchor).Scan(&seconds)
	if err != nil {
		return 0, err
	}
	if seconds < 0 {
		seconds = 0
	}
	return int64(seconds), nil
}

// sumDailyBytes derives usage since the current daily period start, same
// exclusion rule as quota (service <> 'hotspot', FR-58.3's PPPoE-quota
// posture — a daily cap is a plan ceiling like quota, not a graph).
func sumDailyBytes(ctx context.Context, db *pgxpool.Pool, subscriberID string, since time.Time) (int64, error) {
	var bytes int64
	err := db.QueryRow(ctx,
		`SELECT COALESCE(SUM(delta_down + delta_up), 0)
		   FROM usage_points
		  WHERE subscriber_id = $1::uuid AND service <> 'hotspot' AND time >= $2::timestamptz`,
		subscriberID, since).Scan(&bytes)
	return bytes, err
}
