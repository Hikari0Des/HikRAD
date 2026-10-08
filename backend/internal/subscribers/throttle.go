package subscribers

// Portal throttle-status read (v4-11, FR-166: "current effective speed,
// including a plain-language explanation when it is reduced: which limit,
// and when it restores"). A single-subscriber, portal-scoped sibling to
// limitprogress.go's list-row batch query — deliberately its OWN query
// rather than a change to that one: limitprogress.go is v4-5's delicate,
// already-tuned batch path for the panel subscriber list, and touching it
// for an unrelated portal feature risks exactly the kind of regression
// known-issues.md's 2026-08-18 row (v4-7 declining to touch this package's
// loader) already flags as the wrong move. Same frozen evaluation order
// (expiry → quota → uptime → daily, first exhausted wins) as
// radius/limits.go and limitprogress.go, re-derived independently again —
// the established pattern for this package/radius boundary, not a
// duplication bug.
//
// Decision 21 discipline: ThrottleStatus never carries a ceiling, a
// remaining balance, or a percentage — only WHICH limit is active and, only
// where genuinely computable (the daily window), WHEN it lifts.

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

// ThrottleReason is which limit is currently in force, or "" when none is.
type ThrottleReason string

const (
	ThrottleNone    ThrottleReason = ""
	ThrottleExpiry  ThrottleReason = "expiry"
	ThrottleQuota   ThrottleReason = "quota"
	ThrottleUptime  ThrottleReason = "uptime"
	ThrottleDaily   ThrottleReason = "daily"
)

// ThrottleStatus is GET /portal/me's speed-explanation payload.
type ThrottleStatus struct {
	Reason ThrottleReason `json:"reason,omitempty"`
	// Action is what the profile actually does once this limit is
	// exhausted (throttle | block | switch_plan | pool) — the portal's
	// plain-language copy is chosen client-side from Reason+Action, not
	// server-templated, so translators own the exact wording per locale.
	Action string `json:"action,omitempty"`
	// ResetsAt is set ONLY for the daily limit, whose reset time is a real,
	// known clock event (daily_reset_min, Asia/Baghdad) — quota/uptime reset
	// on renewal, not on a schedule, so there is no honest countdown to show
	// for them (Decision 21: never imply a ceiling/remaining figure exists).
	ResetsAt *time.Time `json:"resets_at,omitempty"`
}

// LoadThrottleStatus reports the single first-exhausted limit for one
// subscriber, or a zero-value ThrottleStatus (Reason == ThrottleNone) when
// nothing is currently limiting them.
func LoadThrottleStatus(ctx context.Context, db pgxQuerier, subscriberID string) (ThrottleStatus, error) {
	var (
		isExpired                          bool
		quotaMode, quotaBehavior           string
		quotaTotal, quotaDown, quotaUp     int64
		quotaUsedDown, quotaUsedUp         int64
		uptimeLimit, uptimeUsed            int64
		uptimeAction                       string
		dailyLimit, dailyUsed              int64
		dailyAction                        string
		dailyResetMin                      int
	)
	err := db.QueryRow(ctx, `
		SELECT COALESCE(s.expires_at IS NOT NULL AND s.expires_at < now(), false),
		       COALESCE(p.quota_mode, 'unlimited'), COALESCE(p.quota_behavior, ''),
		       COALESCE(p.quota_total_bytes, 0), COALESCE(p.quota_down_bytes, 0), COALESCE(p.quota_up_bytes, 0),
		       COALESCE(qu.used_down, 0), COALESCE(qu.used_up, 0),
		       COALESCE(p.uptime_limit_seconds, 0), COALESCE(uu.used, 0), COALESCE(p.uptime_exhaust_action, ''),
		       COALESCE(p.daily_limit_bytes, 0), COALESCE(du.used, 0), COALESCE(p.daily_exhaust_action, ''),
		       COALESCE(p.daily_reset_min, 0)
		  FROM subscribers s
		  LEFT JOIN profiles p ON p.id = s.profile_id
		  LEFT JOIN subscriber_counters sc ON sc.subscriber_id = s.id
		  LEFT JOIN LATERAL (
		      SELECT SUM(delta_down) AS used_down, SUM(delta_up) AS used_up FROM usage_points
		       WHERE subscriber_id = s.id AND service <> 'hotspot' AND time >= s.quota_cycle_anchor
		  ) qu ON true
		  LEFT JOIN LATERAL (
		      SELECT SUM(delta_down + delta_up) AS used FROM usage_points
		       WHERE subscriber_id = s.id AND service <> 'hotspot'
		         AND time >= COALESCE(sc.daily_period_start, s.created_at)
		  ) du ON true
		  LEFT JOIN LATERAL (
		      SELECT FLOOR(COALESCE(SUM(EXTRACT(EPOCH FROM (
		               COALESCE(stopped_at, now()) - GREATEST(started_at, COALESCE(sc.uptime_cycle_anchor, s.created_at))
		             ))), 0))::bigint AS used
		        FROM sessions
		       WHERE subscriber_id = s.id AND started_at IS NOT NULL
		         AND COALESCE(stopped_at, now()) > COALESCE(sc.uptime_cycle_anchor, s.created_at)
		  ) uu ON true
		 WHERE s.id = $1::uuid`, subscriberID).Scan(
		&isExpired, &quotaMode, &quotaBehavior, &quotaTotal, &quotaDown, &quotaUp, &quotaUsedDown, &quotaUsedUp,
		&uptimeLimit, &uptimeUsed, &uptimeAction, &dailyLimit, &dailyUsed, &dailyAction, &dailyResetMin)
	if err == pgx.ErrNoRows {
		return ThrottleStatus{}, nil
	}
	if err != nil {
		return ThrottleStatus{}, err
	}

	quotaExhausted := quotaCrossed(quotaMode, quotaTotal, quotaDown, quotaUp, quotaUsedDown, quotaUsedUp)
	uptimeExhausted := uptimeLimit > 0 && uptimeUsed >= uptimeLimit
	dailyExhausted := dailyLimit > 0 && dailyUsed >= dailyLimit

	switch {
	case isExpired:
		return ThrottleStatus{Reason: ThrottleExpiry, Action: "block"}, nil
	case quotaExhausted:
		return ThrottleStatus{Reason: ThrottleQuota, Action: quotaBehavior}, nil
	case uptimeExhausted:
		return ThrottleStatus{Reason: ThrottleUptime, Action: uptimeAction}, nil
	case dailyExhausted:
		reset := nextDailyReset(time.Now(), dailyResetMin)
		return ThrottleStatus{Reason: ThrottleDaily, Action: dailyAction, ResetsAt: &reset}, nil
	default:
		return ThrottleStatus{}, nil
	}
}

// nextDailyReset returns the next occurrence of dailyResetMin (minutes past
// midnight, Asia/Baghdad — NFR product timezone) at or after now.
func nextDailyReset(now time.Time, dailyResetMin int) time.Time {
	loc, err := time.LoadLocation("Asia/Baghdad")
	if err != nil {
		loc = time.UTC
	}
	local := now.In(loc)
	midnight := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
	reset := midnight.Add(time.Duration(dailyResetMin) * time.Minute)
	if !reset.After(local) {
		reset = reset.Add(24 * time.Hour)
	}
	return reset.UTC()
}

// pgxQuerier is the minimal surface LoadThrottleStatus needs, matching the
// *pgxpool.Pool method it actually calls — kept as an interface only so a
// future caller with a *pgx.Tx (e.g. inside another transaction) can reuse
// it without this file importing pgxpool just to name the concrete type.
type pgxQuerier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}
