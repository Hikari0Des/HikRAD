package radius

// Runtime enforcement worker (contract C4, FR-9/FR-10). Phase 2 made
// expiry/quota behaviors apply at *auth time*; this worker makes them apply to
// *already-online sessions*. It subscribes to the two frozen Redis pub/sub
// channels — enforce.quota_exceeded (C publishes on interim evaluation) and
// enforce.expired (D's expiry sweep publishes) — and for each affected
// subscriber with live sessions resolves the profile's configured behavior via
// D's AuthView, then ENQUEUES the matching CoA for every session (v4-8,
// FR-150: enqueue is the only way to send a CoA — this worker decides WHAT to
// send, never dispatches it itself). coa_dispatcher.go owns the actual UDP
// exchange, the retry budget, and the hybrid update→disconnect fallback; this
// worker's job ends the moment a coa_outbox row exists.
//
// Every enforcement decision is still idempotent (re-delivered events are a
// no-op within the cycle, guarded in Postgres so it survives a restart) and
// audited via the enforcement_actions row this worker writes.

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/hikrad/hikrad/internal/auth"
	"github.com/hikrad/hikrad/internal/live/livestate"
	"github.com/hikrad/hikrad/internal/radius/vendor"
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
// even after the FR-15.4 disconnect fallback. C's health page reads it,
// mirroring EnforcementFailuresKey's contract. As of v4-8 it is incremented
// by coa_dispatcher.go when a coa_outbox row resolves to `failed` — the only
// place a CoA's real, final outcome is known — rather than by this worker
// (or billing/live's enqueue call sites), which now only knows whether the
// row was ENQUEUED, not whether it ultimately succeeded.
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

// enforceRecord is the outcome persisted to enforcement_actions and audited.
// Outcome/Applied/Failures now describe ENQUEUE success, not CoA delivery —
// "applied" (all sessions' CoA rows enqueued), "enqueue_failed" (none could
// be enqueued, e.g. DB down), "partial" (mixed).
type enforceRecord struct {
	Key      string // dedup_key of the claim row this finalizes
	Kind     enforceKind
	SubID    string
	Behavior string
	Sessions int
	Applied  int
	Failures int
	Outcome  string // enqueued | partial | enqueue_failed | no_sessions | not_found | noop
	Detail   []sessionOutcome
}

type sessionOutcome struct {
	AcctSessionID string `json:"acct_session_id"`
	Action        string `json:"action"`           // "update" | "disconnect"
	Outcome       string `json:"outcome"`           // enqueued | enqueue_failed
	OutboxID      string `json:"outbox_id,omitempty"`
}

// worker enforces one crossing at a time. Every external dependency is a seam
// so the decide/enqueue logic is unit-testable with no Redis, DB or NAS.
type worker struct {
	log *slog.Logger

	listSessions func(ctx context.Context, subscriberID string) ([]SessionRef, error)
	behavior     func(ctx context.Context, username string) (behaviorView, bool, error)
	// enqueue is the ONLY way this worker ever causes a CoA to be sent
	// (FR-150 gate item 1) — it inserts a coa_outbox row and returns
	// immediately; coa_dispatcher.go does the rest.
	enqueue func(ctx context.Context, ref SessionRef, action CoAAction, attrs []vendor.Attr, reason, subscriberID string) (string, error)
	// claim inserts the idempotency row for key and reports proceed=true only for
	// the first delivery within the cycle.
	claim  func(ctx context.Context, kind enforceKind, subID, key string) (proceed bool, err error)
	record func(ctx context.Context, rec enforceRecord)
	now    func() time.Time
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

// handle processes one crossing end to end: resolve the plan, enqueue it for
// every live session, record the outcome.
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

	action, attrs, label := planSteps(kind, b)
	if action == "" {
		// No actionable behavior (e.g. unknown quota mode) — record and stop.
		w.record(ctx, enforceRecord{Key: key, Kind: kind, SubID: subID, Behavior: label, Sessions: len(sessions), Outcome: "noop"})
		return
	}

	rec := w.enqueueSessions(ctx, kind, subID, label, action, attrs, sessions)
	rec.Key = key
	w.record(ctx, rec)
}

// enqueueSessions enqueues one coa_outbox row per session, all carrying the
// same action/attrs (identical enforcement decision applied to every live
// session of the crossing subscriber).
func (w *worker) enqueueSessions(ctx context.Context, kind enforceKind, subID, label string, action CoAAction, attrs []vendor.Attr, sessions []SessionRef) enforceRecord {
	rec := enforceRecord{Kind: kind, SubID: subID, Behavior: label, Sessions: len(sessions)}
	reason := "enforce." + string(kind) + ":" + label
	for _, s := range sessions {
		id, err := w.enqueue(ctx, s, action, attrs, reason, subID)
		oc := sessionOutcome{AcctSessionID: s.AcctSessionID, Action: string(action)}
		if err != nil {
			oc.Outcome = "enqueue_failed"
			rec.Failures++
			w.log.Warn("enforce: coa enqueue failed", "error", err, "subscriber", subID, "kind", kind)
		} else {
			oc.Outcome = "enqueued"
			oc.OutboxID = id
			rec.Applied++
		}
		rec.Detail = append(rec.Detail, oc)
	}
	switch {
	case rec.Failures == 0:
		rec.Outcome = "enqueued"
	case rec.Applied == 0:
		rec.Outcome = "enqueue_failed"
	default:
		rec.Outcome = "partial"
	}
	return rec
}

// planSteps maps an event kind + behavior to ONE CoA-Update/Disconnect
// enqueue (v4-8 collapses the old sequential move_pool→apply_rate steps into
// a single combined attribute set — the dispatcher's hybrid fallback then
// applies to that one row as a unit rather than to each step separately, and
// a NAK is genuinely all-or-nothing at the NAS for a single packet anyway).
// action == "" means "no actionable behavior" (the old empty-steps sentinel).
// Pure — the enforcement matrix is exercised directly in tests.
func planSteps(kind enforceKind, b behaviorView) (action CoAAction, attrs []vendor.Attr, label string) {
	switch kind {
	case kindQuota:
		switch b.QuotaBehavior {
		case "block":
			return CoAActionDisconnect, nil, "block"
		case "throttle":
			if b.ThrottleRate == "" {
				// No throttle rate configured: disconnecting is the only safe way
				// to stop an over-quota session keeping full speed.
				return CoAActionDisconnect, nil, "throttle"
			}
			return CoAActionUpdate, []vendor.Attr{{Intent: vendor.IntentRateLimit, Value: b.ThrottleRate}}, "throttle"
		case "expired_pool":
			if b.ExpiredPoolName == "" {
				return CoAActionDisconnect, nil, "expired_pool"
			}
			return CoAActionUpdate, []vendor.Attr{{Intent: vendor.IntentAddressPool, Value: b.ExpiredPoolName}}, "expired_pool"
		default:
			return "", nil, b.QuotaBehavior
		}
	case kindExpired:
		if b.ExpiryBehavior == "expired_pool" && b.ExpiredPoolName != "" {
			rate := b.ThrottleRate
			if rate == "" {
				rate = expiredPoolFallbackRate
			}
			// Move to the walled garden AND clamp to a minimal rate in ONE
			// CoA-Update; if the NAS NAKs it, the dispatcher's hybrid
			// fallback disconnects so re-auth applies both at once.
			return CoAActionUpdate, []vendor.Attr{
				{Intent: vendor.IntentAddressPool, Value: b.ExpiredPoolName},
				{Intent: vendor.IntentRateLimit, Value: rate},
			}, "expired_pool"
		}
		// hard block (or expired_pool misconfigured with no pool).
		return CoAActionDisconnect, nil, "block"
	default:
		return "", nil, ""
	}
}

// --- production wiring ------------------------------------------------------

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

// enforceMaxConcurrent bounds how many subscribers' enforcement plans run at
// once (storm safety, CoA hardening task 4): a burst far exceeding steady
// state (e.g. a midnight expiry sweep crossing hundreds of subscribers within
// one Redis publish burst) fans out across a small worker pool instead of
// serializing enqueue calls one after another.
const enforceMaxConcurrent = 16

// newWorker composes the production seams from the module's engine, live state,
// package CoA enqueue API and DB.
func (m *module) newWorker() *worker {
	return &worker{
		log:          m.log,
		listSessions: func(ctx context.Context, subID string) ([]SessionRef, error) { return liveSessionRefs(ctx, m.rdb, subID) },
		behavior:     m.enforceBehavior,
		enqueue:      Enqueue,
		claim:        func(ctx context.Context, kind enforceKind, subID, key string) (bool, error) { return claimEnforcement(ctx, m.db, kind, subID, key) },
		record:       func(ctx context.Context, rec enforceRecord) { recordEnforcement(ctx, m.db, m.log, rec) },
		now:          time.Now,
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
// runs on a context detached from the caller's enforcement-cycle budget so
// the outcome record + audit entry is never lost to a slow enqueue path.
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

// EnforcementFailures reads the CoA-failure counter (C's health page). A nil
// Redis client (unit context) returns an empty map, not an error.
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
