package subscribers

// Counter display (v4-5, FR-134 gate item 1): the user page must show BOTH
// counter families, labelled distinctly, so an operator never wonders which
// number they are looking at. "This cycle" is the v4-4 RESETTABLE state
// (subscriber_counters + a live sum since its anchor — the same derivation
// radius/limits.go's sumUptimeSeconds/sumDailyBytes and accounting/quota.go's
// evaluator already use); "lifetime" is the v4-5 running total
// (subscriber_billing_counters) a renewal/reset NEVER touches.

import (
	"context"
	"time"
)

// counterSummary is the detail-page shape for both counter families.
type counterSummary struct {
	Cycle    cycleCounters    `json:"cycle"`
	Lifetime lifetimeCounters `json:"lifetime"`
}

// cycleCounters is the resettable state, derived live — zero when the
// subscriber has no configured limit of that kind (the query still runs; a
// subscriber with an unlimited profile simply shows 0 used against no ceiling,
// which the frontend renders as "no limit" rather than "0 of 0").
type cycleCounters struct {
	QuotaUsedBytes    int64 `json:"quota_used_bytes"`
	UptimeUsedSeconds int64 `json:"uptime_used_seconds"`
	DailyUsedBytes    int64 `json:"daily_used_bytes"`
}

// lifetimeCounters is the append-only running total — never written by a
// reset (enforced by billingcounters_guard_test.go), whatever cycleCounters
// shows.
type lifetimeCounters struct {
	BytesDown      int64      `json:"bytes_down"`
	BytesUp        int64      `json:"bytes_up"`
	SessionSeconds int64      `json:"session_seconds"`
	FirstSeenAt    *time.Time `json:"first_seen_at"`
	LastSeenAt     *time.Time `json:"last_seen_at"`
}

// loadCounters composes both families for one subscriber. Errors degrade to
// zero values (a display concern, never worth failing the whole detail page
// over) rather than being surfaced — mirrors loadProfileSummary/loadOwner's
// own best-effort posture on this same endpoint.
func (m *Module) loadCounters(ctx context.Context, subscriberID string) counterSummary {
	var out counterSummary

	var quotaCycleAnchor time.Time
	_ = m.db.QueryRow(ctx, `SELECT quota_cycle_anchor FROM subscribers WHERE id = $1::uuid`, subscriberID).
		Scan(&quotaCycleAnchor)

	_ = m.db.QueryRow(ctx,
		`SELECT COALESCE(SUM(delta_down + delta_up), 0)
		   FROM usage_points
		  WHERE subscriber_id = $1::uuid AND service <> 'hotspot' AND time >= $2::timestamptz`,
		subscriberID, quotaCycleAnchor).Scan(&out.Cycle.QuotaUsedBytes)

	if uptimeAnchor, dailyStart, err := ensureCounters(ctx, m.db, subscriberID); err == nil {
		if secs, err := sumUptimeSeconds(ctx, m.db, subscriberID, uptimeAnchor); err == nil {
			out.Cycle.UptimeUsedSeconds = secs
		}
		if bytes, err := sumDailyBytes(ctx, m.db, subscriberID, dailyStart); err == nil {
			out.Cycle.DailyUsedBytes = bytes
		}
	}

	_ = m.db.QueryRow(ctx,
		`SELECT lifetime_bytes_down, lifetime_bytes_up, lifetime_session_seconds, first_seen_at, last_seen_at
		   FROM subscriber_billing_counters WHERE subscriber_id = $1::uuid`, subscriberID).
		Scan(&out.Lifetime.BytesDown, &out.Lifetime.BytesUp, &out.Lifetime.SessionSeconds,
			&out.Lifetime.FirstSeenAt, &out.Lifetime.LastSeenAt)

	return out
}
