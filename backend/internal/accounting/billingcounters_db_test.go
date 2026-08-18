package accounting

// DB-gated proof of FR-134's lifetime counters (v4-5, migration 0602):
// accrual matches usage_points/sessions exactly, and — the gate's central
// claim — a subsequent quota/uptime reset (subscriber_counters, v4-4's table)
// never moves the lifetime figures. Gated on HIKRAD_TEST_DB_URL /
// HIKRAD_TEST_REDIS_URL exactly like the rest of this package's DB suite;
// skips cleanly when unset (this sandbox has neither).

import (
	"testing"
	"time"
)

func TestLifetimeCounters_AccrueAcrossStartInterimStop(t *testing.T) {
	s, ctx, done := testEnv(t)
	defer done()

	nasIP := testIP()
	insertTestNAS(t, ctx, s, nasIP, "pppoe")
	uname := "lifetime-" + runID()
	sub := insertTestSubscriber(t, ctx, s, uname)
	sess := runID()
	base := time.Now().Add(-1 * time.Hour).UTC()

	post(t, s, map[string]any{
		"record_type": "start", "nas_ip": nasIP, "acct_session_id": sess,
		"username": uname, "event_time": evTime(base, 0),
	})
	post(t, s, map[string]any{
		"record_type": "interim", "nas_ip": nasIP, "acct_session_id": sess,
		"username": uname, "event_time": evTime(base, 300),
		"bytes_in": 1000, "bytes_out": 2000,
	})
	post(t, s, map[string]any{
		"record_type": "stop", "nas_ip": nasIP, "acct_session_id": sess,
		"username": uname, "event_time": evTime(base, 600),
		"bytes_in": 1500, "bytes_out": 3000,
	})
	drainAll(t, ctx, s)

	// A second session for the same subscriber, so the test also proves
	// accrual ACROSS sessions, not just within one.
	sess2 := runID()
	post(t, s, map[string]any{
		"record_type": "start", "nas_ip": nasIP, "acct_session_id": sess2,
		"username": uname, "event_time": evTime(base, 700),
	})
	post(t, s, map[string]any{
		"record_type": "interim", "nas_ip": nasIP, "acct_session_id": sess2,
		"username": uname, "event_time": evTime(base, 1000),
		"bytes_in": 5000, "bytes_out": 8000,
	})
	post(t, s, map[string]any{
		"record_type": "stop", "nas_ip": nasIP, "acct_session_id": sess2,
		"username": uname, "event_time": evTime(base, 1300),
		"bytes_in": 5000, "bytes_out": 8000,
	})
	drainAll(t, ctx, s)

	var down, up, seconds int64
	if err := s.db.QueryRow(ctx,
		`SELECT lifetime_bytes_down, lifetime_bytes_up, lifetime_session_seconds
		   FROM subscriber_billing_counters WHERE subscriber_id = $1::uuid`, sub).
		Scan(&down, &up, &seconds); err != nil {
		t.Fatalf("query lifetime counters: %v", err)
	}
	// Acct-Input/Output-Octets are CUMULATIVE-since-session-start counters
	// (RFC 2866), not per-record deltas — advance() computes the delta from
	// the previous reading. Session 1: interim(in=1000,out=2000) then
	// stop(in=1500,out=3000) → deltas (1000,2000) then (500,1000) → down =
	// 2000+1000=3000, up = 1000+500=1500. Session 2: interim(in=5000,
	// out=8000) then stop repeating the SAME cumulative reading → its delta
	// is zero (no new traffic) → down=8000, up=5000. Total: down=11000,
	// up=6500. bytes_out is the download direction (insertUsagePoint's dDown).
	if down != 11000 {
		t.Errorf("lifetime_bytes_down = %d, want 11000", down)
	}
	if up != 6500 {
		t.Errorf("lifetime_bytes_up = %d, want 6500", up)
	}
	if seconds != 1200 {
		t.Errorf("lifetime_session_seconds = %d, want 1200 (600+600 across two sessions)", seconds)
	}

	// Now reset the RESETTABLE counters (subscriber_counters) exactly as a
	// renewal/limits-worker reset does, and prove the lifetime row is
	// byte-for-byte unchanged — the gate's central claim.
	if _, err := s.db.Exec(ctx,
		`INSERT INTO subscriber_counters (subscriber_id, uptime_cycle_anchor, daily_period_start)
		 VALUES ($1::uuid, now(), now())
		 ON CONFLICT (subscriber_id) DO UPDATE SET
		   uptime_cycle_anchor = EXCLUDED.uptime_cycle_anchor,
		   daily_period_start = EXCLUDED.daily_period_start`, sub); err != nil {
		t.Fatalf("reset subscriber_counters: %v", err)
	}

	var down2, up2, seconds2 int64
	if err := s.db.QueryRow(ctx,
		`SELECT lifetime_bytes_down, lifetime_bytes_up, lifetime_session_seconds
		   FROM subscriber_billing_counters WHERE subscriber_id = $1::uuid`, sub).
		Scan(&down2, &up2, &seconds2); err != nil {
		t.Fatalf("query lifetime counters after reset: %v", err)
	}
	if down2 != down || up2 != up || seconds2 != seconds {
		t.Fatalf("lifetime counters changed after a resettable-counter reset: before=(%d,%d,%d) after=(%d,%d,%d)",
			down, up, seconds, down2, up2, seconds2)
	}
}

func TestLifetimeCounters_ReaperCreditsClosedSession(t *testing.T) {
	s, ctx, done := testEnv(t)
	defer done()

	nasIP := testIP()
	insertTestNAS(t, ctx, s, nasIP, "pppoe")
	uname := "reaper-lifetime-" + runID()
	sub := insertTestSubscriber(t, ctx, s, uname)
	sess := runID()
	base := time.Now().Add(-1 * time.Hour).UTC()

	post(t, s, map[string]any{
		"record_type": "start", "nas_ip": nasIP, "acct_session_id": sess,
		"username": uname, "event_time": evTime(base, 0),
	})
	drainAll(t, ctx, s)

	// Backdate the session so it is old enough to reap AND has a real
	// (non-zero) span: started 10 minutes before its last interim.
	startedAt := base.Add(-10 * time.Minute)
	lastInterim := base
	if _, err := s.db.Exec(ctx,
		`UPDATE sessions SET last_interim_at = $2, started_at = $3
		   WHERE nas_id = (SELECT id FROM nas WHERE ip = $1::inet) AND acct_session_id = $4`,
		nasIP, lastInterim, startedAt, sess); err != nil {
		t.Fatalf("backdate session: %v", err)
	}

	// reapAfter = 3*interimInterval + 5min; "now" must clear that threshold
	// measured from last_interim_at (lastInterim = base), not from startedAt.
	s.interimInterval = 1 * time.Second
	if err := s.reapOnce(ctx, lastInterim.Add(400*time.Second)); err != nil {
		t.Fatalf("reap: %v", err)
	}

	var seconds int64
	if err := s.db.QueryRow(ctx,
		`SELECT COALESCE(lifetime_session_seconds,0) FROM subscriber_billing_counters WHERE subscriber_id = $1::uuid`,
		sub).Scan(&seconds); err != nil {
		t.Fatalf("query: %v", err)
	}
	if seconds != 600 {
		t.Fatalf("lifetime_session_seconds = %d, want 600 (the reaped session's full 10-minute span)", seconds)
	}

	var reaped bool
	if err := s.db.QueryRow(ctx,
		`SELECT reaped FROM sessions WHERE nas_id = (SELECT id FROM nas WHERE ip = $1::inet) AND acct_session_id = $2`,
		nasIP, sess).Scan(&reaped); err != nil {
		t.Fatalf("query session: %v", err)
	}
	if !reaped {
		t.Fatal("session was not marked reaped")
	}
}
