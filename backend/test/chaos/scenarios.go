package main

// Scenario implementations (FR-37.5, NFR-2, sub-PRD 03 §7 edge cases). Every
// scenario: (1) starts hikrad-acct fresh, (2) provisions its own NAS so
// counts never collide across runs, (3) floods, (4) injects the named
// failure mid-flood, (5) waits for the FR-40 invariant to hold with in_queue
// back at 0, (6) reconciles Postgres session/usage rows against what was
// actually sent — proving both the counter math AND session-state
// consistency survive, per the DoD.

import (
	"context"
	"fmt"
	"time"
)

func (r *Rig) runScenario(ctx context.Context, name string) (*ScenarioResult, error) {
	started := time.Now()
	var res *ScenarioResult
	var err error
	switch name {
	case "kill-postgres":
		res, err = r.scenarioKillDep(ctx, name, r.PGContainer, 10*time.Second)
	case "kill-redis":
		res, err = r.scenarioKillDep(ctx, name, r.RedisContainer, 10*time.Second)
	case "kill-acct":
		res, err = r.scenarioKillAcct(ctx)
	case "unclean-reboot":
		res, err = r.scenarioUncleanReboot(ctx)
	case "retransmit-storm":
		res, err = r.scenarioRetransmitStorm(ctx)
	case "out-of-order":
		res, err = r.scenarioOutOfOrder(ctx)
	case "panel-down":
		res, err = r.scenarioPanelDown(ctx)
	case "spill-corruption":
		res, err = r.scenarioSpillCorruption(ctx)
	case "redis-durability":
		res, err = r.scenarioRedisDurability(ctx)
	default:
		return nil, fmt.Errorf("unknown scenario %q", name)
	}
	if res != nil {
		res.Name = name
		res.StartedAt = started
		res.Elapsed = time.Since(started)
	}
	return res, err
}

// ensureAcctRunning restarts hikrad-acct if it isn't already up (fresh start
// for a scenario, or recovery after a previous scenario killed it).
func (r *Rig) ensureAcctRunning() error {
	if r.cmd != nil {
		return nil
	}
	return r.startAcct()
}

// resetForNextScenario gives every scenario in `-scenario all` a clean
// pipeline_counters/Redis slate. Without this, scenarios that don't kill the
// acct process (kill-postgres, kill-redis) leave it running across scenario
// boundaries, so the NEXT scenario's counters keep accumulating on top of
// the previous one's totals — its own before/after delta math stays locally
// correct (monotonic counters), but a scenario that DOES restart acct
// mid-run (kill-acct, unclean-reboot) reloads from whatever the PREVIOUS
// scenario left in Postgres/Redis, not zero, which is confusing to read and,
// worse, timing-sensitive leftovers (an earlier scenario's not-quite-drained
// stream backlog still landing) can shift a later scenario's window. Each
// scenario already gets its own fresh NAS id for data-table isolation; this
// does the same for the global counters/stream. Not needed before the first
// scenario (already zero on a freshly provisioned DB).
func (r *Rig) resetForNextScenario(ctx context.Context) error {
	if err := r.killAcct(); err != nil {
		return err
	}
	if _, err := r.db.Exec(ctx, `UPDATE pipeline_counters SET
		received=0, enqueued=0, spilled=0, drained=0, persisted=0,
		deduplicated=0, reaped=0, orphan_stops=0, updated_at=now() WHERE id`); err != nil {
		return err
	}
	if err := r.rdb.FlushDB(ctx).Err(); err != nil {
		return err
	}
	return r.startAcct()
}

// scenarioKillDep floods, kills a docker-managed dependency (Postgres or
// Redis) partway through for r.KillFor, restarts it, then proves recovery.
// The ingest HTTP call itself never fails for either dependency by design
// (Postgres is only touched async by the consumer; Redis-down falls back to
// the disk spill) — so no client-side retries are expected here; a nonzero
// FailedFinal is itself a finding.
func (r *Rig) scenarioKillDep(ctx context.Context, name, container string, retryBudget time.Duration) (*ScenarioResult, error) {
	if err := r.ensureAcctRunning(); err != nil {
		return nil, err
	}
	nasID, nasIP, err := r.provisionNAS(ctx, "pppoe")
	if err != nil {
		return nil, err
	}
	before, err := fetchCounters(r.AcctAddr)
	if err != nil {
		return nil, err
	}
	sessions := newSessions(r.Sessions, nasIP, r.Interims)

	killed := make(chan error, 1)
	go func() {
		time.Sleep(r.Duration / 4)
		killed <- dockerKill(container)
		time.Sleep(r.KillFor)
		killed <- dockerStart(container)
	}()

	flood := runFlood(FloodOpts{
		AcctAddr:    r.AcctAddr,
		Sessions:    sessions,
		Rate:        r.Rate,
		Duration:    r.Duration,
		RetryBudget: retryBudget,
	})
	if err := <-killed; err != nil {
		return nil, fmt.Errorf("docker kill %s: %w", container, err)
	}
	if err := <-killed; err != nil {
		return nil, fmt.Errorf("docker start %s: %w", container, err)
	}
	// Give the just-restarted dependency a moment to accept connections again
	// before polling the invariant.
	time.Sleep(3 * time.Second)

	after, drained := waitInvariant(r.AcctAddr, 90*time.Second)
	pass := drained && flood.FailedFinal == 0 && after.Persisted-before.Persisted == flood.UniqueRecords
	detail := fmt.Sprintf("sent=%d acked=%d failed_final=%d persisted_delta=%d in_queue=%d invariant_ok=%v",
		flood.UniqueRecords, flood.Acked, flood.FailedFinal, after.Persisted-before.Persisted, after.InQueue, after.InvariantOK)

	sessCount, usageCount, serr := r.sessionCounts(ctx, nasID)
	extra := map[string]any{"nas_id": nasID, "sessions_rows": sessCount, "usage_points_rows": usageCount}
	if serr == nil && sessCount != int64(len(sessions)) {
		pass = false
		detail += fmt.Sprintf(" session_rows_mismatch=got:%d want:%d", sessCount, len(sessions))
	}

	return &ScenarioResult{Pass: pass, Detail: detail, Flood: &flood, Counters: after, Extra: extra}, nil
}

// scenarioKillAcct SIGKILLs the hikrad-acct process itself mid-flood — the
// case where the ingest HTTP endpoint really does become unreachable, so the
// flood's own retry-until-ack loop is what proves losslessness (standing in
// for the NAS retransmit the real deployment relies on).
func (r *Rig) scenarioKillAcct(ctx context.Context) (*ScenarioResult, error) {
	if err := r.ensureAcctRunning(); err != nil {
		return nil, err
	}
	nasID, nasIP, err := r.provisionNAS(ctx, "pppoe")
	if err != nil {
		return nil, err
	}
	before, err := fetchCounters(r.AcctAddr)
	if err != nil {
		return nil, err
	}
	sessions := newSessions(r.Sessions, nasIP, r.Interims)

	go func() {
		time.Sleep(r.Duration / 4)
		_ = r.killAcct()
		time.Sleep(r.KillFor)
		_ = r.startAcct()
	}()

	flood := runFlood(FloodOpts{
		AcctAddr:    r.AcctAddr,
		Sessions:    sessions,
		Rate:        r.Rate,
		Duration:    r.Duration,
		RetryBudget: r.KillFor + 30*time.Second,
	})

	after, drained := waitInvariant(r.AcctAddr, 90*time.Second)
	pass := drained && flood.FailedFinal == 0 && after.Persisted-before.Persisted == flood.UniqueRecords
	detail := fmt.Sprintf("sent=%d acked=%d failed_final=%d persisted_delta=%d in_queue=%d (proves 'acct restart resumes backlog')",
		flood.UniqueRecords, flood.Acked, flood.FailedFinal, after.Persisted-before.Persisted, after.InQueue)

	sessCount, usageCount, _ := r.sessionCounts(ctx, nasID)
	extra := map[string]any{"nas_id": nasID, "sessions_rows": sessCount, "usage_points_rows": usageCount}
	if sessCount != int64(len(sessions)) {
		pass = false
		detail += fmt.Sprintf(" session_rows_mismatch=got:%d want:%d", sessCount, len(sessions))
	}
	return &ScenarioResult{Pass: pass, Detail: detail, Flood: &flood, Counters: after, Extra: extra}, nil
}

// scenarioUncleanReboot SIGKILLs Postgres, Redis AND the acct process
// simultaneously (models a hard host power loss, not an orderly shutdown)
// then brings everything back and proves recovery — NFR-2's "unclean
// shutdown must not corrupt data".
func (r *Rig) scenarioUncleanReboot(ctx context.Context) (*ScenarioResult, error) {
	if err := r.ensureAcctRunning(); err != nil {
		return nil, err
	}
	nasID, nasIP, err := r.provisionNAS(ctx, "pppoe")
	if err != nil {
		return nil, err
	}
	before, err := fetchCounters(r.AcctAddr)
	if err != nil {
		return nil, err
	}
	sessions := newSessions(r.Sessions, nasIP, r.Interims)

	go func() {
		time.Sleep(r.Duration / 4)
		// Simultaneous SIGKILL of every component — no graceful shutdown
		// anywhere, matching an actual power-loss reboot.
		_ = dockerKill(r.PGContainer)
		_ = dockerKill(r.RedisContainer)
		_ = r.killAcct()
		time.Sleep(r.KillFor)
		_ = dockerStart(r.PGContainer)
		_ = dockerStart(r.RedisContainer)
		_ = waitTCP(pgHostPort(r.DBURL), 30*time.Second)
		_ = waitTCP(redisHostPort(r.RedisURL), 30*time.Second)
		_ = r.startAcct()
	}()

	flood := runFlood(FloodOpts{
		AcctAddr:    r.AcctAddr,
		Sessions:    sessions,
		Rate:        r.Rate,
		Duration:    r.Duration,
		RetryBudget: r.KillFor + 45*time.Second,
	})

	after, drained := waitInvariant(r.AcctAddr, 120*time.Second)
	pass := drained && flood.FailedFinal == 0 && after.Persisted-before.Persisted == flood.UniqueRecords
	detail := fmt.Sprintf("sent=%d acked=%d failed_final=%d persisted_delta=%d in_queue=%d invariant_ok=%v (all 3 components hard-killed simultaneously)",
		flood.UniqueRecords, flood.Acked, flood.FailedFinal, after.Persisted-before.Persisted, after.InQueue, after.InvariantOK)

	sessCount, usageCount, _ := r.sessionCounts(ctx, nasID)
	extra := map[string]any{"nas_id": nasID, "sessions_rows": sessCount, "usage_points_rows": usageCount}
	if sessCount != int64(len(sessions)) {
		pass = false
		detail += fmt.Sprintf(" session_rows_mismatch=got:%d want:%d", sessCount, len(sessions))
	}
	return &ScenarioResult{Pass: pass, Detail: detail, Flood: &flood, Counters: after, Extra: extra}, nil
}

// scenarioRetransmitStorm sends each record 3x back-to-back (no chaos
// injected) — proves FR-37.4 dedup: persisted counts unique records once,
// deduplicated accounts for exactly the 2 extra deliveries each.
func (r *Rig) scenarioRetransmitStorm(ctx context.Context) (*ScenarioResult, error) {
	if err := r.ensureAcctRunning(); err != nil {
		return nil, err
	}
	nasID, nasIP, err := r.provisionNAS(ctx, "pppoe")
	if err != nil {
		return nil, err
	}
	before, err := fetchCounters(r.AcctAddr)
	if err != nil {
		return nil, err
	}
	// A smaller pool keeps the 3x amplification within the rate budget.
	n := r.Sessions
	if n > 50 {
		n = 50
	}
	sessions := newSessions(n, nasIP, r.Interims)

	flood := runFlood(FloodOpts{
		AcctAddr:       r.AcctAddr,
		Sessions:       sessions,
		Rate:           r.Rate,
		Duration:       r.Duration,
		RetransmitEach: 3,
		RetryBudget:    10 * time.Second,
	})

	after, drained := waitInvariant(r.AcctAddr, 60*time.Second)
	wantDedup := flood.UniqueRecords * 2
	gotDedup := after.Deduplicated - before.Deduplicated
	pass := drained && flood.FailedFinal == 0 &&
		after.Persisted-before.Persisted == flood.UniqueRecords &&
		gotDedup == wantDedup
	detail := fmt.Sprintf("unique_records=%d persisted_delta=%d dedup_delta=%d (want %d)",
		flood.UniqueRecords, after.Persisted-before.Persisted, gotDedup, wantDedup)

	sessCount, usageCount, _ := r.sessionCounts(ctx, nasID)
	extra := map[string]any{"nas_id": nasID, "sessions_rows": sessCount, "usage_points_rows": usageCount}
	return &ScenarioResult{Pass: pass, Detail: detail, Flood: &flood, Counters: after, Extra: extra}, nil
}

// scenarioOutOfOrder delivers a session's interims out of chronological
// order (and one backdated) and proves every event-time-keyed usage point
// still lands (FR-37.4's out-of-order tolerance).
func (r *Rig) scenarioOutOfOrder(ctx context.Context) (*ScenarioResult, error) {
	if err := r.ensureAcctRunning(); err != nil {
		return nil, err
	}
	nasID, nasIP, err := r.provisionNAS(ctx, "pppoe")
	if err != nil {
		return nil, err
	}
	before, err := fetchCounters(r.AcctAddr)
	if err != nil {
		return nil, err
	}

	base := time.Now().Add(-2 * time.Hour).UTC()
	sess := &simSession{nasIP: nasIP, acctID: "chaos-ooo-" + randHex(4), user: "chaos-ooo-user-" + randHex(4), base: base}
	// Deliver: start, interim@90s, interim@30s (earlier, arrives late),
	// interim@10s backdated below the session start clock skew tolerance,
	// stop@120s. All four interim/stop event-times must be distinct rows.
	order := []recordStep{
		{typ: "start", secs: 0},
		{typ: "interim", secs: 90, bytesIn: 9000, bytesOut: 18000},
		{typ: "interim", secs: 30, bytesIn: 3000, bytesOut: 6000},
		{typ: "interim", secs: 10, bytesIn: 1000, bytesOut: 2000},
		{typ: "stop", secs: 120, bytesIn: 12000, bytesOut: 24000},
	}
	var attempts int64
	for _, step := range order {
		rec := buildRecord(sess, step)
		ok, n, _ := postRecord(r.AcctAddr, rec, 10*time.Second)
		attempts += n
		if !ok {
			return &ScenarioResult{Pass: false, Detail: "record not acked: " + step.typ}, nil
		}
	}
	flood := FloodResult{Attempts: attempts, UniqueRecords: int64(len(order)), Acked: int64(len(order))}

	after, drained := waitInvariant(r.AcctAddr, 30*time.Second)
	_, usageCount, _ := r.sessionCounts(ctx, nasID)
	pass := drained && after.Persisted-before.Persisted == int64(len(order)) && usageCount >= 4
	detail := fmt.Sprintf("delivered_out_of_order=%d persisted_delta=%d usage_points_rows=%d (want >=4 distinct event-time rows)",
		len(order), after.Persisted-before.Persisted, usageCount)
	extra := map[string]any{"nas_id": nasID, "usage_points_rows": usageCount}
	return &ScenarioResult{Pass: pass, Detail: detail, Flood: &flood, Counters: after, Extra: extra}, nil
}

// scenarioPanelDown proves NFR-2 / AC-NFR2a's accounting half: the whole
// rig never starts hikrad-api at all, so a clean flood run here is itself
// the proof that accounting has zero runtime dependency on the panel
// process. (The auth-continues half of AC-NFR2a is B/D's RADIUS-path
// territory, out of this agent's exclusive paths.)
func (r *Rig) scenarioPanelDown(ctx context.Context) (*ScenarioResult, error) {
	if err := waitTCP("127.0.0.1:8080", 500*time.Millisecond); err == nil {
		return &ScenarioResult{Pass: false, Detail: "something is listening on :8080 (hikrad-api) — this scenario requires the panel to be absent"}, nil
	}
	if err := r.ensureAcctRunning(); err != nil {
		return nil, err
	}
	nasID, nasIP, err := r.provisionNAS(ctx, "pppoe")
	if err != nil {
		return nil, err
	}
	before, err := fetchCounters(r.AcctAddr)
	if err != nil {
		return nil, err
	}
	sessions := newSessions(min(r.Sessions, 100), nasIP, r.Interims)
	flood := runFlood(FloodOpts{AcctAddr: r.AcctAddr, Sessions: sessions, Rate: r.Rate, Duration: r.Duration / 2, RetryBudget: 10 * time.Second})
	after, drained := waitInvariant(r.AcctAddr, 30*time.Second)
	pass := drained && flood.FailedFinal == 0 && after.Persisted-before.Persisted == flood.UniqueRecords
	detail := fmt.Sprintf("hikrad-api never started; persisted_delta=%d (accounting unaffected by panel absence)", after.Persisted-before.Persisted)
	extra := map[string]any{"nas_id": nasID}
	return &ScenarioResult{Pass: pass, Detail: detail, Flood: &flood, Counters: after, Extra: extra}, nil
}
