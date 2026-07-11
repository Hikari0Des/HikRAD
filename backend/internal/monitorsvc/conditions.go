package monitorsvc

// Periodic (non-probe) alert conditions evaluated in the monitor process:
// disk_low, acct_backlog (depth + the FR-40 invariant), radius_reject_spike,
// agent_balance_low, and the scheduled digests. Each condition asks the alert
// engine to fire its rule type with a `match` closure over the observed metric,
// so per-rule thresholds/cooldowns/quiet-hours all flow through the same engine.

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/hikrad/hikrad/internal/platform"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

type conditions struct {
	db       *pgxpool.Pool
	rdb      *redis.Client
	settings platform.Settings
	alerts   *alertEngine
	log      *slog.Logger
	now      func() time.Time
	loc      *time.Location
}

func newConditions(db *pgxpool.Pool, rdb *redis.Client, settings platform.Settings, a *alertEngine, log *slog.Logger) *conditions {
	return &conditions{db: db, rdb: rdb, settings: settings, alerts: a, log: log, now: time.Now, loc: baghdad}
}

// evaluate runs every condition once. Called on the monitor's condition tick.
func (c *conditions) evaluate(ctx context.Context) {
	c.diskLow(ctx)
	c.acctBacklog(ctx)
	c.rejectSpike(ctx)
	c.agentBalanceLow(ctx)
	c.digests(ctx)
}

func (c *conditions) diskLow(ctx context.Context) {
	for _, d := range diskUsageAll() {
		used := d.UsedPercent
		path := d.Path
		c.alerts.Fire(ctx, fireInput{
			ruleType: "disk_low", state: "firing",
			summary: fmt.Sprintf("Disk %s at %.0f%% used", path, used),
			payload: map[string]any{"path": path, "used_percent": used},
			match:   func(r rule) bool { return used >= numFromThreshold(r, "percent", 85) },
		})
	}
}

func (c *conditions) acctBacklog(ctx context.Context) {
	snap := acctSnapshot(ctx, c.db, c.rdb)
	if snap == nil {
		return
	}
	depth, _ := toInt64(snap["in_queue"])
	invOK, _ := snap["invariant_ok"].(bool)

	// Depth rules (no "invariant" flag): backlog exceeded.
	c.alerts.Fire(ctx, fireInput{
		ruleType: "acct_backlog", state: "firing",
		summary: fmt.Sprintf("Accounting backlog: %d records queued", depth),
		payload: map[string]any{"depth": depth},
		match: func(r rule) bool {
			if isInvariantRule(r) {
				return false
			}
			return float64(depth) >= numFromThreshold(r, "depth", 1000)
		},
	})
	// Invariant rules: the M2 conservation check broke.
	if !invOK {
		c.alerts.Fire(ctx, fireInput{
			ruleType: "acct_backlog", state: "firing",
			summary: "Accounting pipeline invariant BROKEN (received − dup − queued ≠ persisted)",
			payload: map[string]any{"invariant_ok": false},
			match:   func(r rule) bool { return isInvariantRule(r) },
		})
	}
}

func (c *conditions) rejectSpike(ctx context.Context) {
	total, rejects := decisionCounts(ctx, c.rdb, time.Minute)
	if total < 10 {
		return // too little traffic to judge a spike
	}
	rate := float64(rejects) / float64(total)
	c.alerts.Fire(ctx, fireInput{
		ruleType: "radius_reject_spike", state: "firing",
		summary: fmt.Sprintf("RADIUS reject rate %.0f%% (%d/%d in last minute)", rate*100, rejects, total),
		payload: map[string]any{"rate": rate, "rejects": rejects, "total": total},
		match:   func(r rule) bool { return rate >= numFromThreshold(r, "rate", 0.5) },
	})
}

// agentBalanceLow is best-effort: agent balances are D's ledger domain and the
// exact source isn't frozen for C, so it reads a balance view IF present and
// otherwise no-ops (degrades cleanly while D's schema lands).
func (c *conditions) agentBalanceLow(ctx context.Context) {
	if c.db == nil {
		return
	}
	rows, err := c.db.Query(ctx,
		`SELECT manager_id::text, name, balance_iqd FROM manager_balances`)
	if err != nil {
		return // view absent → skip
	}
	defer rows.Close()
	type bal struct {
		id, name string
		amount   int64
	}
	var bals []bal
	for rows.Next() {
		var b bal
		if err := rows.Scan(&b.id, &b.name, &b.amount); err != nil {
			return
		}
		bals = append(bals, b)
	}
	for _, b := range bals {
		amt := float64(b.amount)
		name := b.name
		c.alerts.Fire(ctx, fireInput{
			ruleType: "agent_balance_low", state: "firing",
			summary: fmt.Sprintf("Agent %s balance low: %d IQD", name, b.amount),
			payload: map[string]any{"manager_id": b.id, "balance_iqd": b.amount},
			match:   func(r rule) bool { return amt <= numFromThreshold(r, "min_iqd", 0) },
		})
	}
}

// digests fires the daily expiring/business digest once per day at the configured
// hour (Asia/Baghdad, default 08:00), guarded by a per-rule daily claim so a
// crash-restart within the hour doesn't double-send.
func (c *conditions) digests(ctx context.Context) {
	now := c.now().In(c.loc)
	rules, err := c.alerts.loadRules(ctx, "expiring_digest")
	if err != nil {
		return
	}
	for _, r := range rules {
		hour := int(numFromThreshold(r, "hour", 8))
		if now.Hour() != hour {
			continue
		}
		if !c.claimDaily(ctx, r.ID, now) {
			continue
		}
		days := int(numFromThreshold(r, "days", 3))
		summary := c.digestSummary(ctx, days)
		// Fire with a match that always accepts this specific rule (single fire).
		c.alerts.Fire(ctx, fireInput{
			ruleType: "expiring_digest", state: "firing",
			summary: summary,
			payload: map[string]any{"days": days},
			match:   func(rr rule) bool { return rr.ID == r.ID },
		})
	}
}

// digestSummary composes the one-line digest: subscribers expiring in N days plus
// today's business figures (active count + revenue from D's revenue_daily).
func (c *conditions) digestSummary(ctx context.Context, days int) string {
	var expiring, active int64
	_ = c.db.QueryRow(ctx,
		`SELECT
		   count(*) FILTER (WHERE status='active' AND expires_at IS NOT NULL
		                    AND expires_at >= now() AND expires_at < now() + ($1 || ' days')::interval),
		   count(*) FILTER (WHERE status='active')
		 FROM subscribers`, days).Scan(&expiring, &active)
	revenue := revenueTodayDB(ctx, c.db)
	return fmt.Sprintf("Daily digest — %d subscribers expiring in %d days · %d active · %d IQD collected today",
		expiring, days, active, revenue)
}

func (c *conditions) claimDaily(ctx context.Context, ruleID string, now time.Time) bool {
	if c.rdb == nil {
		return true
	}
	key := "alert:digest:" + ruleID + ":" + now.Format("2006-01-02")
	ok, err := c.rdb.SetNX(ctx, key, "1", 25*time.Hour).Result()
	if err != nil {
		return true
	}
	return ok
}

// --- helpers ----------------------------------------------------------------

func isInvariantRule(r rule) bool {
	v, ok := r.Threshold["invariant"].(bool)
	return ok && v
}

// numFromThreshold reads a numeric threshold field (JSON numbers decode to
// float64), returning def when absent or non-numeric.
func numFromThreshold(r rule, key string, def float64) float64 {
	if r.Threshold == nil {
		return def
	}
	switch v := r.Threshold[key].(type) {
	case float64:
		return v
	case json.Number:
		if f, err := v.Float64(); err == nil {
			return f
		}
	}
	return def
}

// decisionCounts tallies total + reject decisions in the last window from the
// radius:decisions stream (read-only consumer).
func decisionCounts(ctx context.Context, rdb *redis.Client, window time.Duration) (total, rejects int) {
	if rdb == nil {
		return 0, 0
	}
	c, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	sinceMS := time.Now().Add(-window).UnixMilli()
	entries, err := rdb.XRevRangeN(c, decisionStream, "+", "-", 2000).Result()
	if err != nil {
		return 0, 0
	}
	for _, e := range entries {
		if ms := streamIDMillis(e.ID); ms > 0 && ms < sinceMS {
			break
		}
		total++
		if raw, ok := e.Values["event"].(string); ok {
			var ev struct {
				Outcome string `json:"outcome"`
			}
			if json.Unmarshal([]byte(raw), &ev) == nil && ev.Outcome == "reject" {
				rejects++
			}
		}
	}
	return total, rejects
}

// revenueTodayDB reads today's revenue from D's revenue_daily view against an
// explicit handle (the monitor's), tolerating an as-yet-missing view/column.
func revenueTodayDB(ctx context.Context, db *pgxpool.Pool) int64 {
	if db == nil {
		return 0
	}
	for _, col := range []string{"revenue_iqd", "amount_iqd", "total_iqd", "revenue", "amount"} {
		var v int64
		err := db.QueryRow(ctx,
			`SELECT COALESCE(SUM(`+col+`),0) FROM revenue_daily
			  WHERE day = (now() AT TIME ZONE 'Asia/Baghdad')::date`).Scan(&v)
		if err == nil {
			return v
		}
		if !isUndefinedTable(err) {
			return 0
		}
	}
	return 0
}
