package radius

// Runtime enforcement worker (contract C4, FR-9/FR-10). Phase 2 made
// expiry/quota behaviors apply at *auth time*; this worker makes them apply to
// *already-online sessions*. It subscribes to the two frozen Redis pub/sub
// channels — enforce.quota_exceeded (C publishes on interim evaluation) and
// enforce.expired (D's expiry sweep publishes) — and for each affected
// subscriber with live sessions resolves the profile's configured behavior via
// D's AuthView, then executes the matching CoA on every session within one
// cycle (≤ 5 min):
//
//	quota:  block → Disconnect · throttle → ApplyRate(throttle) · expired_pool → MovePool
//	expiry: block → Disconnect · expired_pool → MovePool + minimal rate
//
// A CoA that NAKs on an in-place change falls back to Disconnect (FR-15.4) so
// the NAS re-auths and the auth-time half applies the same behavior. Every
// enforcement is idempotent (re-delivered events are a no-op within the cycle,
// guarded in Postgres so it survives a restart), audited, and — on unrecoverable
// CoA failure — counted into the enforcement_failures Redis counter C's health
// surfaces.

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/hikrad/hikrad/internal/auth"
	"github.com/hikrad/hikrad/internal/live/livestate"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

// Frozen pub/sub channels (contract C4).
const (
	chanQuotaExceeded = "enforce.quota_exceeded"
	chanExpired       = "enforce.expired"
)

// EnforcementFailuresKey is the Redis counter of CoA enforcements that failed
// even after the FR-15.4 disconnect fallback. C's health/alerts read it
// (rule type acct_backlog-style). Frozen key name (contract: B exposes it).
const EnforcementFailuresKey = "enforce:failures"

// enforceKind distinguishes the two channels and is the event_kind recorded.
type enforceKind string

const (
	kindQuota   enforceKind = "quota_exceeded"
	kindExpired enforceKind = "expired"
)

// enforceEvent is the pub/sub payload on both channels.
type enforceEvent struct {
	SubscriberID string `json:"subscriber_id"`
}

// behaviorView is the slice of AuthView the worker acts on.
type behaviorView struct {
	Username        string
	QuotaBehavior   string
	ExpiryBehavior  string
	ThrottleRate    string
	ExpiredPoolName string
}

// coaStep is one CoA operation in a session's enforcement plan. fallback marks
// an in-place change (throttle / move-pool) that, on NAK/timeout, falls back to
// Disconnect (FR-15.4) so re-auth picks up the behavior instead.
type coaStep struct {
	op       string // "disconnect" | "apply_rate" | "move_pool"
	param    string
	fallback bool
}

// enforceRecord is the outcome persisted to enforcement_actions and audited.
type enforceRecord struct {
	Key      string // dedup_key of the claim row this finalizes
	Kind     enforceKind
	SubID    string
	Behavior string
	Sessions int
	Applied  int
	Failures int
	Outcome  string // applied | partial | failed | no_sessions | not_found | noop
	Detail   []sessionOutcome
}

type sessionOutcome struct {
	AcctSessionID string `json:"acct_session_id"`
	Path          string `json:"path"`    // e.g. "apply_rate", "apply_rate→disconnect"
	Outcome       string `json:"outcome"` // ack | nak | timeout | error
}

// worker enforces one crossing at a time. Every external dependency is a seam
// so the mapping/idempotency/fallback logic is unit-testable with no Redis, DB
// or NAS.
type worker struct {
	log *slog.Logger

	listSessions func(ctx context.Context, subscriberID string) ([]SessionRef, error)
	behavior     func(ctx context.Context, username string) (behaviorView, bool, error)
	disconnect   func(ctx context.Context, ref SessionRef) CoAResult
	applyRate    func(ctx context.Context, ref SessionRef, rate string) CoAResult
	movePool     func(ctx context.Context, ref SessionRef, pool string) CoAResult
	// claim inserts the idempotency row for key and reports proceed=true only for
	// the first delivery within the cycle.
	claim       func(ctx context.Context, kind enforceKind, subID, key string) (proceed bool, err error)
	record      func(ctx context.Context, rec enforceRecord)
	incFailures func(ctx context.Context, n int)
	sleep       func(d time.Duration)
	now         func() time.Time
	maxAttempts int
}

// cycle is the enforcement window that dedups re-delivered events; it matches
// the ≤ 5 min guarantee and D/C's interim cadence.
const enforceCycle = 5 * time.Minute

// dedupKey buckets an event so re-delivery within one cycle is suppressed but a
// genuinely new crossing in a later cycle is not.
func (w *worker) dedupKey(kind enforceKind, subID string) string {
	bucket := w.now().Unix() / int64(enforceCycle/time.Second)
	return string(kind) + ":" + subID + ":" + itoa(int(bucket))
}

// handle processes one crossing end to end.
func (w *worker) handle(ctx context.Context, kind enforceKind, subID string) {
	if subID == "" {
		return
	}
	key := w.dedupKey(kind, subID)
	proceed, err := w.claim(ctx, kind, subID, key)
	if err != nil {
		w.log.Warn("enforce: dedup claim failed", "error", err, "subscriber", subID, "kind", kind)
		return
	}
	if !proceed {
		// Already enforced this cycle — a re-delivered / repeated event.
		return
	}

	sessions, err := w.listSessions(ctx, subID)
	if err != nil {
		w.log.Warn("enforce: list sessions failed", "error", err, "subscriber", subID)
		return
	}
	if len(sessions) == 0 {
		// Offline subscriber: nothing to enforce. Not an error (edge case).
		w.record(ctx, enforceRecord{Key: key, Kind: kind, SubID: subID, Outcome: "no_sessions"})
		return
	}

	b, found, err := w.behavior(ctx, sessions[0].Username)
	if err != nil {
		w.log.Warn("enforce: resolve behavior failed", "error", err, "subscriber", subID)
		return
	}
	if !found {
		w.record(ctx, enforceRecord{Key: key, Kind: kind, SubID: subID, Sessions: len(sessions), Outcome: "not_found"})
		return
	}

	steps, label := planSteps(kind, b)
	if len(steps) == 0 {
		// No actionable behavior (e.g. unknown quota mode) — record and stop.
		w.record(ctx, enforceRecord{Key: key, Kind: kind, SubID: subID, Behavior: label, Sessions: len(sessions), Outcome: "noop"})
		return
	}

	rec := w.enforceSessions(ctx, kind, subID, label, steps, sessions)
	rec.Key = key
	w.record(ctx, rec)
	if rec.Failures > 0 {
		w.incFailures(ctx, rec.Failures)
		w.log.Warn("enforce: coa failures (alert-worthy)",
			"subscriber", subID, "kind", kind, "behavior", label,
			"failures", rec.Failures, "sessions", rec.Sessions)
	}
}

// enforceSessions runs the plan against every session, retrying the ones that
// still failed with backoff up to maxAttempts.
func (w *worker) enforceSessions(ctx context.Context, kind enforceKind, subID, label string, steps []coaStep, sessions []SessionRef) enforceRecord {
	rec := enforceRecord{Kind: kind, SubID: subID, Behavior: label, Sessions: len(sessions)}
	done := make([]bool, len(sessions))
	outcomes := make([]sessionOutcome, len(sessions))

	attempts := w.maxAttempts
	if attempts < 1 {
		attempts = 1
	}
	for attempt := 0; attempt < attempts; attempt++ {
		remaining := 0
		for i, s := range sessions {
			if done[i] {
				continue
			}
			oc := w.runPlan(ctx, s, steps)
			outcomes[i] = oc
			if oc.Outcome == "ack" {
				done[i] = true
			} else {
				remaining++
			}
		}
		if remaining == 0 {
			break
		}
		if attempt < attempts-1 {
			// Exponential-ish backoff before retrying the still-failed sessions.
			w.sleep(time.Duration(attempt+1) * 200 * time.Millisecond)
		}
	}

	for i := range sessions {
		if done[i] {
			rec.Applied++
		} else {
			rec.Failures++
		}
		rec.Detail = append(rec.Detail, outcomes[i])
	}
	switch {
	case rec.Failures == 0:
		rec.Outcome = "applied"
	case rec.Applied == 0:
		rec.Outcome = "failed"
	default:
		rec.Outcome = "partial"
	}
	return rec
}

// runPlan executes one session's steps in order. A fallback step that NAKs is
// retried as a Disconnect (FR-15.4); a successful fallback-disconnect ends the
// session so remaining steps are moot. Returns the terminal outcome + the path
// actually taken.
func (w *worker) runPlan(ctx context.Context, s SessionRef, steps []coaStep) sessionOutcome {
	oc := sessionOutcome{AcctSessionID: s.AcctSessionID}
	for _, step := range steps {
		res := w.runStep(ctx, s, step)
		oc.Path = joinPath(oc.Path, step.op)
		if res.Ok() {
			oc.Outcome = string(res.Outcome)
			continue
		}
		if step.fallback {
			// In-place change refused: fall back to Disconnect so re-auth applies
			// the behavior. Record which path ran.
			fb := w.disconnect(ctx, s)
			oc.Path = joinPath(oc.Path, "disconnect")
			oc.Outcome = string(fb.Outcome)
			return oc
		}
		oc.Outcome = string(res.Outcome)
		return oc
	}
	return oc
}

func (w *worker) runStep(ctx context.Context, s SessionRef, step coaStep) CoAResult {
	switch step.op {
	case "disconnect":
		return w.disconnect(ctx, s)
	case "apply_rate":
		return w.applyRate(ctx, s, step.param)
	case "move_pool":
		return w.movePool(ctx, s, step.param)
	default:
		return CoAResult{Outcome: CoAError}
	}
}

func joinPath(a, b string) string {
	if a == "" {
		return b
	}
	return a + "→" + b
}

// planSteps maps an event kind + behavior to the ordered CoA plan and a label.
// Pure — the enforcement matrix is exercised directly in tests.
func planSteps(kind enforceKind, b behaviorView) (steps []coaStep, label string) {
	switch kind {
	case kindQuota:
		switch b.QuotaBehavior {
		case "block":
			return []coaStep{{op: "disconnect"}}, "block"
		case "throttle":
			if b.ThrottleRate == "" {
				// No throttle rate configured: disconnecting is the only safe way
				// to stop an over-quota session keeping full speed.
				return []coaStep{{op: "disconnect"}}, "throttle"
			}
			return []coaStep{{op: "apply_rate", param: b.ThrottleRate, fallback: true}}, "throttle"
		case "expired_pool":
			if b.ExpiredPoolName == "" {
				return []coaStep{{op: "disconnect"}}, "expired_pool"
			}
			return []coaStep{{op: "move_pool", param: b.ExpiredPoolName, fallback: true}}, "expired_pool"
		default:
			return nil, b.QuotaBehavior
		}
	case kindExpired:
		if b.ExpiryBehavior == "expired_pool" && b.ExpiredPoolName != "" {
			rate := b.ThrottleRate
			if rate == "" {
				rate = expiredPoolFallbackRate
			}
			// Move to the walled garden, then clamp to a minimal rate. If the
			// move NAKs we fall back to Disconnect and the rate step is moot.
			return []coaStep{
				{op: "move_pool", param: b.ExpiredPoolName, fallback: true},
				{op: "apply_rate", param: rate, fallback: false},
			}, "expired_pool"
		}
		// hard block (or expired_pool misconfigured with no pool).
		return []coaStep{{op: "disconnect"}}, "block"
	default:
		return nil, ""
	}
}

// --- production wiring ------------------------------------------------------

// enforceMaxConcurrent bounds how many subscribers' enforcement plans run at
// once (storm safety, CoA hardening task 4): a burst far exceeding steady
// state (e.g. a midnight expiry sweep crossing hundreds of subscribers within
// one Redis publish burst) fans out across a small worker pool instead of
// serializing one full CoA round trip (up to ~10s worst case with retry)
// behind the next — without this a burst can starve the ≤5min enforcement SLA
// (contract C4) — while coaService's own inflight cap (coaMaxInflight) still
// bounds the NAS-facing packet rate underneath it.
const enforceMaxConcurrent = 16

// startEnforcementWorker subscribes to both channels and dispatches events
// across a bounded worker pool. It runs for the process lifetime (like
// regenerateClients); a nil Redis client (unit context) is a no-op.
func (m *module) startEnforcementWorker(ctx context.Context) {
	if m.rdb == nil {
		return
	}
	w := m.newWorker()
	sub := m.rdb.Subscribe(ctx, chanQuotaExceeded, chanExpired)
	defer func() { _ = sub.Close() }()
	ch := sub.Channel()
	m.log.Info("enforcement worker started", "channels", []string{chanQuotaExceeded, chanExpired})

	sem := make(chan struct{}, enforceMaxConcurrent)
	var wg sync.WaitGroup
	defer wg.Wait()

	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			kind := kindQuota
			if msg.Channel == chanExpired {
				kind = kindExpired
			}
			var ev enforceEvent
			if json.Unmarshal([]byte(msg.Payload), &ev) != nil {
				m.log.Warn("enforce: bad event payload", "channel", msg.Channel, "payload", msg.Payload)
				continue
			}
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return
			}
			wg.Add(1)
			go func(kind enforceKind, subID string) {
				defer wg.Done()
				defer func() { <-sem }()
				// Detach from the subscription context so a slow enforcement
				// can't stall the reader; bound each with its own timeout.
				hctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
				defer cancel()
				w.handle(hctx, kind, subID)
			}(kind, ev.SubscriberID)
		}
	}
}

// newWorker composes the production seams from the module's engine, live state,
// package CoA API and DB.
func (m *module) newWorker() *worker {
	return &worker{
		log:          m.log,
		listSessions: func(ctx context.Context, subID string) ([]SessionRef, error) { return liveSessionRefs(ctx, m.rdb, subID) },
		behavior:     m.enforceBehavior,
		disconnect:   Disconnect,
		applyRate:    ApplyRate,
		movePool:     MovePool,
		claim:        func(ctx context.Context, kind enforceKind, subID, key string) (bool, error) { return claimEnforcement(ctx, m.db, kind, subID, key) },
		record:       func(ctx context.Context, rec enforceRecord) { recordEnforcement(ctx, m.db, m.log, rec) },
		incFailures:  func(ctx context.Context, n int) { incEnforcementFailures(ctx, m.rdb, n) },
		sleep:        time.Sleep,
		now:          time.Now,
		maxAttempts:  3,
	}
}

// enforceBehavior resolves the AuthView slice the worker acts on.
func (m *module) enforceBehavior(ctx context.Context, username string) (behaviorView, bool, error) {
	view, found, err := m.eng.resolveView(ctx, username)
	if err != nil || !found {
		return behaviorView{}, found, err
	}
	return behaviorView{
		Username:        username,
		QuotaBehavior:   view.QuotaBehavior,
		ExpiryBehavior:  view.ExpiryBehavior,
		ThrottleRate:    view.ThrottleRate,
		ExpiredPoolName: view.ExpiredPoolName,
	}, true, nil
}

// liveSessionRefs returns every live session for a subscriber as CoA targets,
// reading C's live-state hash (contract C6). A subscriber's sessions across
// both services are enforced (edge case: multiple live sessions).
func liveSessionRefs(ctx context.Context, rdb *redis.Client, subID string) ([]SessionRef, error) {
	all, err := livestate.All(ctx, rdb)
	if err != nil {
		return nil, err
	}
	var out []SessionRef
	for _, s := range all {
		if s.SubscriberID != subID {
			continue
		}
		out = append(out, SessionRef{
			NASID:         s.NASID,
			AcctSessionID: s.AcctSessionID,
			Username:      s.Username,
			FramedIP:      s.IP,
			Service:       s.Service,
		})
	}
	return out, nil
}

// claimEnforcement inserts the idempotency row; proceed is true only for the
// first delivery of this (kind, subscriber, cycle). DB-level, so idempotency
// survives a restart even though the pub/sub delivery does not.
func claimEnforcement(ctx context.Context, db *pgxpool.Pool, kind enforceKind, subID, key string) (bool, error) {
	if db == nil {
		return true, nil // no DB (degraded): never block enforcement
	}
	var id int64
	err := db.QueryRow(ctx,
		`INSERT INTO enforcement_actions (subscriber_id, event_kind, dedup_key)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (dedup_key) DO NOTHING
		 RETURNING id`, subID, string(kind), key).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil // conflict: already enforced this cycle
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// recordEnforcement finalizes the claim row with the outcome + counts. It
// runs on a context detached from the caller's enforcement-cycle budget: a
// slow/unreachable NAS can burn the whole cycle's timeout in CoA retries
// before we get here, and the outcome record + audit entry (the only trace
// gate item 4 / contract C4 "records outcome to audit" leaves) must not be
// lost just because the CoA phase ran long — mirrors the WithoutCancel
// detach already used one layer up in startEnforcementWorker.
func recordEnforcement(ctx context.Context, db *pgxpool.Pool, log *slog.Logger, rec enforceRecord) {
	if db == nil {
		return
	}
	wctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	detail, _ := json.Marshal(map[string]any{"sessions": rec.Detail})
	_, err := db.Exec(wctx,
		`UPDATE enforcement_actions
		    SET behavior = $2, sessions = $3, applied = $4, failures = $5,
		        outcome = $6, detail = $7, at = now()
		  WHERE dedup_key = $1`,
		rec.Key, rec.Behavior, rec.Sessions, rec.Applied, rec.Failures, rec.Outcome, detail)
	if err != nil {
		if log != nil {
			log.Error("enforce: record outcome failed", "error", err, "subscriber", rec.SubID, "kind", rec.Kind)
		}
		return
	}
	// Per-enforcement audit entry (contract C4: "records outcome to audit").
	_ = auth.Audit(wctx, "enforce."+string(rec.Kind), "subscriber", rec.SubID, nil, map[string]any{
		"behavior": rec.Behavior, "sessions": rec.Sessions,
		"applied": rec.Applied, "failures": rec.Failures, "outcome": rec.Outcome,
	})
}

func incEnforcementFailures(ctx context.Context, rdb *redis.Client, n int) {
	if rdb == nil || n <= 0 {
		return
	}
	_ = rdb.IncrBy(ctx, EnforcementFailuresKey, int64(n)).Err()
}

// EnforcementFailures reads the CoA-failure counter (C's health surface).
func EnforcementFailures(ctx context.Context) (int64, error) {
	e := defaultEngine()
	if e == nil || e.rdb == nil {
		return 0, nil
	}
	n, err := e.rdb.Get(ctx, EnforcementFailuresKey).Int64()
	if err == redis.Nil {
		return 0, nil
	}
	return n, err
}
