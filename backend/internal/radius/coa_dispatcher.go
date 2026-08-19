package radius

// CoA dispatcher (FR-151): the ONLY place that turns a coa_outbox row into a
// real UDP exchange. Runs on its own worker tick, exactly as the phase brief
// specifies — one dispatch attempt per pending/due row per tick, never a
// tight synchronous retry loop, so the row-level retry budget (coa_retry_max)
// is genuinely spread across ticks and visible in the row between them.
//
// One tick does three passes:
//  1. reclaim rows a dead process left `sent` (crash safety, gate item 2).
//  2. claim due `pending` rows -> `sent`, attempt each once, record the
//     outcome (`done` on ACK, `nack` otherwise).
//  3. sweep every row currently `nack` and decide its next move: retry
//     (-> pending), hybrid-fallback to Disconnect (-> pending, current_op
//     flips, fallback_* recorded — gate items 3/4), or give up (-> failed).
//
// Before ever sending anything, a row's target session is checked against
// live state: if it is no longer there, the row resolves `done` (a no-op),
// never `failed` — the reaper-interaction rule (gate item 8) that keeps a
// closed session's queued Disconnect from becoming a false failure on the
// operator's screen.
import (
	"context"
	"fmt"
	"time"

	"github.com/hikrad/hikrad/internal/live/livestate"
	"github.com/redis/go-redis/v9"
	"layeh.com/radius"
)

const (
	coaDispatchInterval    = 3 * time.Second
	coaDispatchBatch       = 100
	coaDispatchConcurrency = 16
	// coaStuckSentAfter is how long a row may sit `sent` before the dispatcher
	// assumes the process that claimed it died mid-exchange and reclaims it.
	// Comfortably above the per-attempt timeout + safety retry so a merely
	// slow NAS is never reclaimed out from under an attempt still in flight.
	coaStuckSentAfter = 30 * time.Second
)

// startCoADispatcher runs coaDispatchTick for the process lifetime, matching
// the enforcement/limits workers' pattern (nil Redis/DB = unit context, no-op).
func (m *module) startCoADispatcher(ctx context.Context) {
	if m.rdb == nil || m.db == nil {
		return
	}
	m.log.Info("coa dispatcher started", "interval", coaDispatchInterval.String())
	ticker := time.NewTicker(coaDispatchInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			tctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
			m.coaDispatchTick(tctx)
			cancel()
		}
	}
}

func (m *module) coaDispatchTick(ctx context.Context) {
	if n, err := reclaimStuckSent(ctx, m.db, coaStuckSentAfter); err != nil {
		m.log.Warn("coa dispatcher: reclaim stuck rows failed", "error", err)
	} else if n > 0 {
		m.log.Warn("coa dispatcher: reclaimed rows stuck in sent (process died mid-exchange?)", "count", n)
	}

	rows, err := claimBatch(ctx, m.db, coaDispatchBatch)
	if err != nil {
		m.log.Warn("coa dispatcher: claim batch failed", "error", err)
	} else if len(rows) > 0 {
		m.dispatchClaimed(ctx, rows)
	}

	if err := m.nackSweep(ctx); err != nil {
		m.log.Warn("coa dispatcher: nack sweep failed", "error", err)
	}
}

// dispatchClaimed attempts every claimed row, bounded by a small local
// concurrency cap (coaService's own inflight cap still bounds the NAS-facing
// packet rate underneath this, same storm-safety posture as enforce.go).
func (m *module) dispatchClaimed(ctx context.Context, rows []OutboxRow) {
	sem := make(chan struct{}, coaDispatchConcurrency)
	done := make(chan struct{}, len(rows))
	for _, row := range rows {
		row := row
		sem <- struct{}{}
		go func() {
			defer func() { <-sem; done <- struct{}{} }()
			m.dispatchOne(ctx, row)
		}()
	}
	for range rows {
		<-done
	}
}

// dispatchOne attempts exactly one CoA/Disconnect exchange for row's current
// operation and persists the outcome.
func (m *module) dispatchOne(ctx context.Context, row OutboxRow) {
	if m.rdb != nil && !sessionStillLive(ctx, m.rdb, row.NASID, row.AcctSessionID) {
		if err := resolveReaped(ctx, m.db, row.ID); err != nil {
			m.log.Warn("coa dispatcher: resolve reaped row failed", "error", err, "id", row.ID)
		}
		return
	}

	code := radius.CodeCoARequest
	attrs := row.Attributes
	if row.CurrentOp == CoAActionDisconnect {
		code = radius.CodeDisconnectRequest
		attrs = nil
	}

	res, wire := m.coa.send(ctx, row.sessionRef(), code, attrs)
	attemptNo := row.AttemptCount + 1
	errText := ""
	if res.Err != nil {
		errText = res.Err.Error()
	}
	if err := insertAttempt(ctx, m.db, row.ID, attemptNo, row.CurrentOp, string(res.Outcome), errText, wire); err != nil {
		m.log.Warn("coa dispatcher: insert attempt failed", "error", err, "id", row.ID)
	}

	if res.Ok() {
		if err := resolveDone(ctx, m.db, row.ID, string(res.Outcome)); err != nil {
			m.log.Warn("coa dispatcher: resolve done failed", "error", err, "id", row.ID)
		}
		return
	}
	if err := resolveNack(ctx, m.db, row.ID, string(res.Outcome), errText); err != nil {
		m.log.Warn("coa dispatcher: resolve nack failed", "error", err, "id", row.ID)
	}
}

// sessionStillLive reports whether (nasID, acctSessionID) still has a live
// entry in C's live-state hash — the reaper-interaction check (gate item 8).
// Mirrors live/coa_action.go's own HGet lookup against the same Redis
// hash/field convention (internal/live/livestate is a dependency-free leaf
// package, not internal/live itself, so this creates no import cycle — the
// enforcement/limits workers already import it the same way).
func sessionStillLive(ctx context.Context, rdb *redis.Client, nasID, acctSessionID string) bool {
	_, err := rdb.HGet(ctx, livestate.HashKey, livestate.Field(nasID, acctSessionID)).Bytes()
	return err == nil
}

// nackSweep decides the next move for every row currently sitting in `nack`.
func (m *module) nackSweep(ctx context.Context) error {
	rows, err := listNackRows(ctx, m.db)
	if err != nil {
		return err
	}
	retryMax := m.coa.retryMax(ctx)
	for _, row := range rows {
		d := decideNack(row, retryMax)
		if err := applyNackDecision(ctx, m.db, d); err != nil {
			m.log.Warn("coa dispatcher: apply nack decision failed", "error", err, "id", row.ID)
			continue
		}
		if d.Fail {
			// The one place a CoA's real, final failure is known — mirrors
			// enforce.go's EnforcementFailuresKey contract (C's health page),
			// now sourced from every enqueuer (worker, renewal, panel) rather
			// than just the enforcement worker's own synchronous outcome.
			incEnforcementFailures(ctx, m.rdb, 1)
			m.log.Warn("coa dispatcher: row failed terminally", "id", row.ID, "username", row.Username, "reason", row.Reason)
		}
	}
	return nil
}

// decideNack is the hybrid-fallback decision (FR-151), pure so it is
// unit-testable without a database: retry while budget remains; on
// exhausting it, an `update` row that hasn't fallen back yet switches to
// Disconnect (visibly, via fallback_used/fallback_reason — gate items 3/4);
// anything else (already a disconnect, or already fell back once) is
// terminal.
func decideNack(row OutboxRow, retryMax int) nackDecision {
	if row.AttemptCount < retryMax {
		return nackDecision{ID: row.ID, Retry: true}
	}
	if row.CurrentOp == CoAActionUpdate && !row.FallbackUsed {
		return nackDecision{
			ID: row.ID, Fallback: true,
			FallbackReason: fmt.Sprintf(
				"CoA-Update NAKed/timed out %d time(s) (retry limit %d) — falling back to Disconnect-Request",
				row.AttemptCount, retryMax),
		}
	}
	return nackDecision{ID: row.ID, Fail: true}
}
