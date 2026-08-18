package radius

// Continuous limits worker (v4-4, FR-128/FR-129/FR-131 §3.6). Extends the
// existing time-driven sweep pattern (tod.go) rather than enforce.go's
// pub/sub reactor: uptime and daily usage accrue continuously while a
// session is open, so there is no natural "crossed" event to publish the
// way D's expiry sweep / quota evaluator publish one — a minute-ish ticker
// over every ONLINE session is the right shape, exactly as it already is
// for the FR-11 TOD sweep.
//
// Deliberately narrow scope: this worker acts ONLY on uptime/daily
// crossings and speed-window sync. Expiry and quota's mid-session
// enforcement stays owned entirely by enforce.go's existing pub/sub worker
// (untouched by this phase) — two workers CoA-ing the same crossing is
// exactly the "two paths that can drift" failure mode the phase brief
// warns against, so evaluateLimits' Kind field is what keeps them apart:
// this worker no-ops whenever Kind is "expiry" or "quota".
//
// Idempotency (gate item 9): punitive actions (uptime/daily exceeded) reuse
// enforce.go's own claim/record machinery verbatim — the identical
// enforcement_actions dedup_key mechanism, restart-safe by construction, so
// a kill mid-sweep and restart cannot double-apply or double-audit a
// crossing. The daily-reset sweep claims per (profile, calendar day) through
// the same table. Speed-window live sync and the "restored" resync after a
// daily reset are NOT independently audited, matching tod.go's own posture
// for its boost/de-boost sweep — an in-memory last-applied-rate map only
// exists to avoid redundant CoA chatter within one running process; it is
// not the idempotency source of truth (the claim is), so forgetting it on
// restart is harmless.
import (
	"context"
	"log/slog"
	"time"

	"github.com/hikrad/hikrad/internal/auth"
	"github.com/hikrad/hikrad/internal/live/livestate"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	kindUptime     enforceKind = "uptime_exceeded"
	kindDaily      enforceKind = "daily_exceeded"
	kindDailyReset enforceKind = "daily_reset"
)

// limitsSweepInterval matches todSweepInterval's cadence (tod.go) — minute
// granularity is fine for minute-grained windows/resets and cheap (one
// livestate.All + one AuthView resolve per online session, cache-hit for the
// overwhelming majority).
const limitsSweepInterval = time.Minute

// dailyResetProfile is one profile the daily-reset sweep must watch.
type dailyResetProfile struct {
	ID       string
	ResetMin int
}

// limitsEngine holds the sweep's seams so the decision/dispatch logic is
// unit-testable with no Redis/DB/NAS. w supplies the proven claim/record/
// runPlan/dedupKey machinery from enforce.go's worker — reused verbatim
// rather than reimplemented, so idempotency and CoA fallback behavior are
// guaranteed identical to expiry/quota's.
type limitsEngine struct {
	log *slog.Logger
	eng *engine
	w   *worker

	sessions           func(ctx context.Context) ([]livestate.State, error)
	dailyResetProfiles func(ctx context.Context) ([]dailyResetProfile, error)
	resetDailyCounters func(ctx context.Context, profileID string) ([]string, error)
	auditProfile       func(ctx context.Context, action, profileID string, detail any)

	// normalRateState/wasEnforced are best-effort de-noise, not the
	// idempotency source of truth (see package doc above): they exist only
	// to avoid re-issuing the identical CoA every tick while nothing changed.
	normalRateState map[string]string
	wasEnforced     map[string]bool
}

func newLimitsEngine(log *slog.Logger, eng *engine, w *worker) *limitsEngine {
	return &limitsEngine{
		log:             log,
		eng:             eng,
		w:               w,
		normalRateState: map[string]string{},
		wasEnforced:     map[string]bool{},
	}
}

// tick runs one full sweep: the daily-reset check first (so a subscriber
// reset THIS tick is immediately re-evaluated and CoA-restored within the
// same tick — gate item 4, "no operator action"), then every online session.
func (e *limitsEngine) tick(ctx context.Context) {
	now := e.eng.now().In(serverLocation())
	e.dailyResetTick(ctx, now)

	sessions, err := e.sessions(ctx)
	if err != nil {
		e.log.Warn("limits: list sessions failed", "error", err)
		return
	}
	for _, s := range sessions {
		e.evaluateSession(ctx, s, now)
	}
}

// evaluateSession resolves one online session's current policy outcome
// (the SAME evaluateLimits/selectSpeedWindow decide() itself calls) and acts
// on it.
func (e *limitsEngine) evaluateSession(ctx context.Context, s livestate.State, now time.Time) {
	if s.Username == "" {
		return
	}
	field := livestate.Field(s.NASID, s.AcctSessionID)
	view, found, err := e.eng.resolveView(ctx, s.Username)
	if err != nil {
		e.log.Warn("limits: resolve view failed", "error", err, "username", s.Username)
		return
	}
	if !found {
		delete(e.normalRateState, field)
		delete(e.wasEnforced, field)
		return
	}
	hotspot := s.Service == "hotspot"
	oc := evaluateLimits(view, hotspot, e.eng.isExpired(view))
	ref := SessionRef{NASID: s.NASID, AcctSessionID: s.AcctSessionID, Username: s.Username, FramedIP: s.IP, Service: s.Service}

	switch oc.Kind {
	case "uptime", "daily":
		e.wasEnforced[field] = true
		delete(e.normalRateState, field)
		e.enforceLimit(ctx, ref, s.SubscriberID, view, oc)
		return
	case "expiry", "quota":
		// Deferred entirely to enforce.go's pub/sub worker; don't track or
		// sync here so a later return to modeNormal is correctly treated as
		// "restored" below rather than silently assumed already-correct.
		delete(e.normalRateState, field)
		delete(e.wasEnforced, field)
		return
	}

	// modeNormal: resolve a currently-active speed window (FR-131) and sync
	// on change. A forced sync fires on the first normal tick after THIS
	// worker last enforced uptime/daily on the session — the "restored, no
	// operator action" guarantee (gate item 4) — rather than a fresh,
	// unseen baseline (which intentionally does not fire, mirroring tod.go:
	// a session already correct from auth time needs no correction).
	var windowRate string
	if w, ok := selectSpeedWindow(view.SpeedWindows, now); ok {
		windowRate = w.Rate
	}
	rate := normalRate(view, hotspot, windowRate)
	prev, seen := e.normalRateState[field]
	forcedRestore := e.wasEnforced[field]
	e.normalRateState[field] = rate
	delete(e.wasEnforced, field)
	if forcedRestore || (seen && prev != rate) {
		res := e.w.applyRate(ctx, ref, rate)
		e.log.Info("limits: rate sync", "username", ref.Username, "rate", rate, "outcome", res.Outcome, "restored", forcedRestore)
	}
}

// enforceLimit applies an exhausted uptime/daily limit's unified action
// (FR-130) to one session, through enforce.go's proven claim → plan → CoA →
// record pipeline (idempotent per gate item 9).
func (e *limitsEngine) enforceLimit(ctx context.Context, ref SessionRef, subID string, view AuthView, oc policyOutcome) {
	kind := kindUptime
	if oc.Kind == "daily" {
		kind = kindDaily
	}
	key := e.w.dedupKey(kind, subID)
	proceed, err := e.w.claim(ctx, kind, subID, key)
	if err != nil {
		e.log.Warn("limits: claim failed", "error", err, "subscriber", subID, "kind", kind)
		return
	}
	if !proceed {
		return // already enforced this cycle
	}

	steps, label := planStepsForOutcome(view, oc)
	rec := enforceRecord{Key: key, Kind: kind, SubID: subID, Behavior: label, Sessions: 1}
	if len(steps) == 0 {
		rec.Outcome = "noop"
	} else {
		so := e.w.runPlan(ctx, ref, steps)
		rec.Detail = []sessionOutcome{so}
		if so.Outcome == string(CoAACK) {
			rec.Applied = 1
			rec.Outcome = "applied"
		} else {
			rec.Failures = 1
			rec.Outcome = "failed"
		}
	}
	e.w.record(ctx, rec)
	if rec.Failures > 0 {
		e.w.incFailures(ctx, rec.Failures)
		e.log.Warn("limits: coa failure (alert-worthy)", "subscriber", subID, "kind", kind, "behavior", label)
	}
}

// planStepsForOutcome maps a resolved policyOutcome to the CoA plan, reusing
// the SAME rate/pool resolution helpers policy.go's replyIntents calls
// (limits.go: throttleOrSwitchRate/expiredPoolRateAndPool) — the auth-time
// reply and the worker's CoA target can never disagree on what "throttled"
// or "switched" means for this subscriber.
func planStepsForOutcome(view AuthView, oc policyOutcome) (steps []coaStep, label string) {
	if oc.Reject {
		return []coaStep{{op: "disconnect"}}, "block"
	}
	switch oc.Mode {
	case modeThrottled:
		return []coaStep{{op: "apply_rate", param: throttleOrSwitchRate(view, oc), fallback: true}}, "throttle"
	case modeSwitchPlan:
		rate := throttleOrSwitchRate(view, oc)
		if oc.Pool == "" {
			return []coaStep{{op: "apply_rate", param: rate, fallback: true}}, "switch_plan"
		}
		return []coaStep{
			{op: "move_pool", param: oc.Pool, fallback: true},
			{op: "apply_rate", param: rate, fallback: false},
		}, "switch_plan"
	case modeExpiredPool:
		rate, pool := expiredPoolRateAndPool(view, oc)
		return []coaStep{
			{op: "move_pool", param: pool, fallback: true},
			{op: "apply_rate", param: rate, fallback: false},
		}, "expired_pool"
	default:
		return nil, ""
	}
}

// dailyResetTick claims and applies the FR-129 daily reset for every profile
// whose daily_reset_min has passed today (server-local) and has not already
// been reset today — restart-safe via the same claim mechanism (gate item
// 9), keyed by (profile, calendar day) rather than the 5-minute enforcement
// cycle bucket since a reset happens at most once per day.
func (e *limitsEngine) dailyResetTick(ctx context.Context, now time.Time) {
	profiles, err := e.dailyResetProfiles(ctx)
	if err != nil {
		e.log.Warn("limits: list daily-limit profiles failed", "error", err)
		return
	}
	mod := minuteOfDay(now)
	day := now.Format("2006-01-02")
	for _, p := range profiles {
		if mod < p.ResetMin {
			continue // today's reset time hasn't arrived yet
		}
		key := "daily_reset:" + p.ID + ":" + day
		proceed, err := e.w.claim(ctx, kindDailyReset, p.ID, key)
		if err != nil {
			e.log.Warn("limits: daily reset claim failed", "error", err, "profile", p.ID)
			continue
		}
		if !proceed {
			continue // already reset today
		}
		subIDs, err := e.resetDailyCounters(ctx, p.ID)
		if err != nil {
			e.log.Warn("limits: daily reset failed", "error", err, "profile", p.ID)
			continue
		}
		if err := InvalidatePolicyByProfile(subIDs); err != nil {
			e.log.Warn("limits: invalidate policy after daily reset failed", "error", err, "profile", p.ID)
		}
		e.auditProfile(ctx, "enforce.daily_reset", p.ID, map[string]any{"subscribers": len(subIDs)})
		e.log.Info("limits: daily reset", "profile", p.ID, "subscribers", len(subIDs))
	}
}

// --- production wiring ------------------------------------------------------

// startLimitsSweep runs the boundary/enforcement check every
// limitsSweepInterval for the process lifetime. A nil Redis client (unit
// context) is a no-op, matching startTODSweeps/startEnforcementWorker.
func (m *module) startLimitsSweep(ctx context.Context) {
	if m.rdb == nil {
		return
	}
	e := newLimitsEngine(m.log, m.eng, m.newWorker())
	e.sessions = func(ctx context.Context) ([]livestate.State, error) { return livestate.All(ctx, m.rdb) }
	e.dailyResetProfiles = func(ctx context.Context) ([]dailyResetProfile, error) {
		return listDailyLimitProfiles(ctx, m.db)
	}
	e.resetDailyCounters = func(ctx context.Context, profileID string) ([]string, error) {
		return resetDailyCountersForProfile(ctx, m.db, profileID)
	}
	e.auditProfile = func(ctx context.Context, action, profileID string, detail any) {
		_ = auth.Audit(ctx, action, "profile", profileID, nil, detail)
	}

	m.log.Info("limits sweep engine started", "interval", limitsSweepInterval.String())
	ticker := time.NewTicker(limitsSweepInterval)
	defer ticker.Stop()
	e.tick(ctx) // establish state immediately rather than waiting a full interval
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			tctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
			e.tick(tctx)
			cancel()
		}
	}
}

// listDailyLimitProfiles reads every profile with an active daily limit —
// the small, cheap set the sweep checks each tick (not every profile).
func listDailyLimitProfiles(ctx context.Context, db *pgxpool.Pool) ([]dailyResetProfile, error) {
	rows, err := db.Query(ctx,
		`SELECT id::text, daily_reset_min FROM profiles WHERE daily_limit_bytes IS NOT NULL AND daily_limit_bytes > 0`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []dailyResetProfile
	for rows.Next() {
		var p dailyResetProfile
		if err := rows.Scan(&p.ID, &p.ResetMin); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// resetDailyCountersForProfile advances daily_period_start to now() for
// every subscriber on profileID — a subscriber_counters row may not exist
// yet (lazily created on first AuthView load), so this upserts rather than
// only updating, guaranteeing every subscriber on the profile is covered
// regardless of whether they had been evaluated before. Returns the affected
// subscriber ids so the caller can invalidate B's policy cache for them.
func resetDailyCountersForProfile(ctx context.Context, db *pgxpool.Pool, profileID string) ([]string, error) {
	rows, err := db.Query(ctx,
		`INSERT INTO subscriber_counters (subscriber_id, daily_period_start)
		   SELECT id, now() FROM subscribers WHERE profile_id = $1::uuid
		 ON CONFLICT (subscriber_id) DO UPDATE SET daily_period_start = EXCLUDED.daily_period_start
		 RETURNING subscriber_id::text`, profileID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
