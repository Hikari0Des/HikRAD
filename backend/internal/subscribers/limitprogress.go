package subscribers

// Per-row limit progress for the subscriber list (v4-5, FR-135): daily bar,
// quota / uptime / days-remaining progress, and the plan-swap badge (current
// plan, plus — when a switch_plan exhaustion action is currently in force —
// the arrow-joined "from → to" pair). This is a READ-ONLY display projection,
// never a decision: it re-derives the same "used vs. limit" comparison
// radius/limits.go's evaluateLimits makes at auth time, in the frozen order
// (expiry → quota → uptime → daily), so the badge names the same limit RADIUS
// itself would act on — but it makes no enforcement decision and is not a
// second implementation of one (nothing here rejects/throttles/CoAs anyone).
//
// Batched across the whole page in a handful of queries (LATERAL joins scoped
// to the page's subscriber ids) rather than one round trip per row — the same
// "two grouped queries, not N+1" discipline profiles/store.go's attachCounts
// already established for the profile list.

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// limitProgress is one subscriber's list-row decoration.
type limitProgress struct {
	SubscriberID string `json:"subscriber_id"`

	QuotaMode       string `json:"quota_mode"`
	QuotaUsedBytes  int64  `json:"quota_used_bytes"` // total mode: combined used
	QuotaUsedDown   int64  `json:"quota_used_down"`  // split mode: download used
	QuotaUsedUp     int64  `json:"quota_used_up"`    // split mode: upload used
	QuotaTotalBytes int64  `json:"quota_total_bytes"`
	QuotaDownBytes  int64  `json:"quota_down_bytes"`
	QuotaUpBytes    int64  `json:"quota_up_bytes"`

	UptimeUsedSeconds  int64 `json:"uptime_used_seconds"`
	UptimeLimitSeconds int64 `json:"uptime_limit_seconds"` // 0 = unlimited

	DailyUsedBytes  int64 `json:"daily_used_bytes"`
	DailyLimitBytes int64 `json:"daily_limit_bytes"` // 0 = unlimited

	// DaysRemaining is nil when the subscriber has no expiry set.
	DaysRemaining *int `json:"days_remaining"`

	// PlanSwap: the plan-swap badge. Active only when the FIRST exhausted
	// limit in the frozen order has action "switch_plan" — matching what
	// RADIUS would actually be doing right now for this subscriber.
	PlanSwap planSwapBadge `json:"plan_swap"`
}

type planSwapBadge struct {
	Active        bool   `json:"active"`
	FromProfileID string `json:"from_profile_id,omitempty"`
	ToProfileID   string `json:"to_profile_id,omitempty"`
	ToProfileName string `json:"to_profile_name,omitempty"`
}

// rawLimitRow is what the batched query returns per subscriber before the
// exhaustion/order logic (loadLimitProgress) turns it into limitProgress.
type rawLimitRow struct {
	subscriberID   string
	profileID      *string
	daysRemaining  *int
	expiresAt      *string // presence only; the days_remaining computation already handles NULL
	expiryBehavior string
	expiryTarget   string
	isExpired      bool

	quotaMode      string
	quotaTotal     int64
	quotaDown      int64
	quotaUp        int64
	quotaUsedDown  int64
	quotaUsedUp    int64
	quotaBehavior  string
	quotaTarget    string
	quotaExhausted bool

	uptimeLimit     int64
	uptimeUsed      int64
	uptimeAction    string
	uptimeTarget    string
	uptimeExhausted bool

	dailyLimit     int64
	dailyUsed      int64
	dailyAction    string
	dailyTarget    string
	dailyExhausted bool
}

// loadLimitProgress batches every field the list needs for the given
// subscriber ids into one query, then applies the frozen evaluation order in
// Go (identical shape to radius/limits.go's evaluateLimits, re-derived here
// for display rather than shared code — the two packages do not import each
// other, matching the existing subscribers/radius seam boundary).
func loadLimitProgress(ctx context.Context, db *pgxpool.Pool, ids []string) (map[string]limitProgress, error) {
	out := map[string]limitProgress{}
	if len(ids) == 0 {
		return out, nil
	}

	rows, err := db.Query(ctx, `
		SELECT s.id::text,
		       s.profile_id::text,
		       CASE WHEN s.expires_at IS NULL THEN NULL
		            ELSE CEIL(EXTRACT(EPOCH FROM (s.expires_at - now())) / 86400.0)::int END,
		       COALESCE(s.expires_at IS NOT NULL AND s.expires_at < now(), false),
		       COALESCE(p.expiry_behavior, ''), COALESCE(p.expiry_exhaust_target, ''),
		       COALESCE(p.quota_mode, 'unlimited'),
		       COALESCE(p.quota_total_bytes, 0), COALESCE(p.quota_down_bytes, 0), COALESCE(p.quota_up_bytes, 0),
		       COALESCE(qu.used_down, 0), COALESCE(qu.used_up, 0),
		       COALESCE(p.quota_behavior, ''), COALESCE(p.quota_exhaust_target, ''),
		       COALESCE(p.uptime_limit_seconds, 0), COALESCE(uu.used, 0),
		       COALESCE(p.uptime_exhaust_action, ''), COALESCE(p.uptime_exhaust_target, ''),
		       COALESCE(p.daily_limit_bytes, 0), COALESCE(du.used, 0),
		       COALESCE(p.daily_exhaust_action, ''), COALESCE(p.daily_exhaust_target, '')
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
		 WHERE s.id = ANY($1::uuid[])`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var raws []rawLimitRow
	switchTargets := map[string]bool{}
	for rows.Next() {
		var r rawLimitRow
		if err := rows.Scan(
			&r.subscriberID, &r.profileID, &r.daysRemaining, &r.isExpired,
			&r.expiryBehavior, &r.expiryTarget,
			&r.quotaMode, &r.quotaTotal, &r.quotaDown, &r.quotaUp, &r.quotaUsedDown, &r.quotaUsedUp,
			&r.quotaBehavior, &r.quotaTarget,
			&r.uptimeLimit, &r.uptimeUsed, &r.uptimeAction, &r.uptimeTarget,
			&r.dailyLimit, &r.dailyUsed, &r.dailyAction, &r.dailyTarget,
		); err != nil {
			return nil, err
		}
		r.quotaExhausted = quotaCrossed(r.quotaMode, r.quotaTotal, r.quotaDown, r.quotaUp, r.quotaUsedDown, r.quotaUsedUp)
		r.uptimeExhausted = r.uptimeLimit > 0 && r.uptimeUsed >= r.uptimeLimit
		r.dailyExhausted = r.dailyLimit > 0 && r.dailyUsed >= r.dailyLimit
		raws = append(raws, r)

		for _, target := range []struct{ action, target string }{
			{r.expiryBehavior, r.expiryTarget}, {r.quotaBehavior, r.quotaTarget},
			{r.uptimeAction, r.uptimeTarget}, {r.dailyAction, r.dailyTarget},
		} {
			if target.action == "switch_plan" && looksLikeUUID(target.target) {
				switchTargets[target.target] = true
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	names, err := profileNames(ctx, db, switchTargets)
	if err != nil {
		return nil, err
	}

	for _, r := range raws {
		lp := limitProgress{
			SubscriberID: r.subscriberID,
			QuotaMode:    r.quotaMode, QuotaUsedBytes: r.quotaUsedDown + r.quotaUsedUp,
			QuotaUsedDown: r.quotaUsedDown, QuotaUsedUp: r.quotaUsedUp, QuotaTotalBytes: r.quotaTotal,
			QuotaDownBytes: r.quotaDown, QuotaUpBytes: r.quotaUp,
			UptimeUsedSeconds: r.uptimeUsed, UptimeLimitSeconds: r.uptimeLimit,
			DailyUsedBytes: r.dailyUsed, DailyLimitBytes: r.dailyLimit,
			DaysRemaining: r.daysRemaining,
		}
		// Frozen order (radius/limits.go): the FIRST exhausted limit wins.
		// Only its own action matters for the badge — later limits are not
		// consulted, exactly mirroring evaluateLimits.
		switch {
		case r.isExpired:
			lp.PlanSwap = swapBadgeFor(r.profileID, r.expiryBehavior, r.expiryTarget, names)
		case r.quotaExhausted:
			lp.PlanSwap = swapBadgeFor(r.profileID, r.quotaBehavior, r.quotaTarget, names)
		case r.uptimeExhausted:
			lp.PlanSwap = swapBadgeFor(r.profileID, r.uptimeAction, r.uptimeTarget, names)
		case r.dailyExhausted:
			lp.PlanSwap = swapBadgeFor(r.profileID, r.dailyAction, r.dailyTarget, names)
		}
		out[r.subscriberID] = lp
	}
	return out, nil
}

// quotaCrossed mirrors accounting/quota.go's evaluator exactly (total: down+up
// vs. one ceiling; split: each direction against its own ceiling) for
// display, without touching the live quota:exhausted overlay flag (that bit
// is RADIUS/accounting's, not this list's to read).
func quotaCrossed(mode string, total, down, up, usedDown, usedUp int64) bool {
	switch mode {
	case "total":
		return total > 0 && usedDown+usedUp >= total
	case "split":
		return (down > 0 && usedDown >= down) || (up > 0 && usedUp >= up)
	default:
		return false
	}
}

func swapBadgeFor(fromProfileID *string, action, target string, names map[string]string) planSwapBadge {
	if action != "switch_plan" || !looksLikeUUID(target) {
		return planSwapBadge{}
	}
	from := ""
	if fromProfileID != nil {
		from = *fromProfileID
	}
	return planSwapBadge{Active: true, FromProfileID: from, ToProfileID: target, ToProfileName: names[target]}
}

func profileNames(ctx context.Context, db *pgxpool.Pool, ids map[string]bool) (map[string]string, error) {
	out := map[string]string{}
	if len(ids) == 0 {
		return out, nil
	}
	list := make([]string, 0, len(ids))
	for id := range ids {
		list = append(list, id)
	}
	rows, err := db.Query(ctx, `SELECT id::text, name FROM profiles WHERE id = ANY($1::uuid[])`, list)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		out[id] = name
	}
	return out, rows.Err()
}
