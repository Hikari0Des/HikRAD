package radius

// Unit tests for the v4-4 continuous limits worker (limitsworker.go):
// uptime/daily mid-session enforcement, idempotency (gate item 9), the
// expiry/quota deferral boundary, speed-window live sync, and the daily
// reset sweep + restore. Reuses enforce_test.go's testWorker/fakeCoA harness
// (same package) so the CoA plumbing under test is identical to expiry/
// quota's proven seams.

import (
	"context"
	"testing"
	"time"

	"github.com/hikrad/hikrad/internal/live/livestate"
)

// newLimitsTestEnv wires a fakeProvider-backed engine (mirrors
// authorize_test.go's newTestEnv, minus the NAS/service plumbing this
// worker never touches) plus a testWorker for the CoA/claim seams.
type limitsTestEnv struct {
	eng      *engine
	provider *fakeProvider
	tw       *testWorker
	le       *limitsEngine

	sessions   []livestate.State
	resetPro   []dailyResetProfile
	resetCalls []string          // profile ids resetDailyCounters was called for
	resetSubs  map[string][]string // profile id -> subscriber ids to return
	audits     []string          // "action:profileID" pairs
}

func newLimitsTestEnv(t *testing.T, now time.Time) *limitsTestEnv {
	t.Helper()
	env := &limitsTestEnv{
		provider:  &fakeProvider{views: map[string]AuthView{}},
		resetSubs: map[string][]string{},
	}
	SetPolicyProvider(env.provider)
	t.Cleanup(func() { SetPolicyProvider(nil) })

	env.eng = &engine{
		log:     discardLogger(),
		now:     func() time.Time { return now },
		decrypt: func(b []byte) ([]byte, error) { return b, nil },
	}
	env.tw = newTestWorker(t)
	env.le = newLimitsEngine(discardLogger(), env.eng, env.tw.worker)
	env.le.sessions = func(context.Context) ([]livestate.State, error) { return env.sessions, nil }
	env.le.dailyResetProfiles = func(context.Context) ([]dailyResetProfile, error) { return env.resetPro, nil }
	env.le.resetDailyCounters = func(_ context.Context, profileID string) ([]string, error) {
		env.resetCalls = append(env.resetCalls, profileID)
		return env.resetSubs[profileID], nil
	}
	env.le.auditProfile = func(_ context.Context, action, profileID string, _ any) {
		env.audits = append(env.audits, action+":"+profileID)
	}
	return env
}

func (env *limitsTestEnv) addSession(username, subID string) {
	env.sessions = append(env.sessions, livestate.State{
		Username: username, SubscriberID: subID, NASID: "nas1", AcctSessionID: subID + "-sess",
	})
}

// --- uptime/daily enforcement + idempotency (gate items 2, 9) -------------

func TestLimitsWorker_UptimeThrottleAppliesRate(t *testing.T) {
	env := newLimitsTestEnv(t, fixedNow)
	v := baseView("s1", "pw")
	v.UptimeLimitSeconds, v.UptimeUsedSeconds = 3600, 3601
	v.UptimeExhaustAction = "throttle"
	v.UptimeThrottleRate = "1M/1M"
	env.provider.views["u"] = v
	env.addSession("u", "s1")

	env.le.tick(context.Background())

	if len(env.tw.coa.rates) != 1 || env.tw.coa.rates[0].rate != "1M/1M" {
		t.Fatalf("expected one apply_rate 1M/1M, got %+v", env.tw.coa.rates)
	}
	if len(env.tw.records) != 1 || env.tw.records[0].Outcome != "applied" {
		t.Fatalf("expected one applied record, got %+v", env.tw.records)
	}
}

func TestLimitsWorker_DailyBlockDisconnects(t *testing.T) {
	env := newLimitsTestEnv(t, fixedNow)
	v := baseView("s1", "pw")
	v.DailyLimitBytes, v.DailyUsedBytes = 1000, 1000
	v.DailyExhaustAction = "block"
	env.provider.views["u"] = v
	env.addSession("u", "s1")

	env.le.tick(context.Background())

	if len(env.tw.coa.disconnects) != 1 {
		t.Fatalf("expected one disconnect, got %+v", env.tw.coa.disconnects)
	}
}

func TestLimitsWorker_Idempotent(t *testing.T) {
	env := newLimitsTestEnv(t, fixedNow)
	v := baseView("s1", "pw")
	v.DailyLimitBytes, v.DailyUsedBytes = 1000, 1000
	v.DailyExhaustAction = "block"
	env.provider.views["u"] = v
	env.addSession("u", "s1")

	env.le.tick(context.Background())
	env.le.tick(context.Background()) // re-run within the same 5-min cycle

	if len(env.tw.coa.disconnects) != 1 {
		t.Fatalf("expected exactly one disconnect across two ticks (idempotent), got %d", len(env.tw.coa.disconnects))
	}
	if len(env.tw.records) != 1 {
		t.Fatalf("expected exactly one enforcement record, got %d", len(env.tw.records))
	}
}

func TestLimitsWorker_DeferredToEnforceGoForExpiry(t *testing.T) {
	env := newLimitsTestEnv(t, fixedNow)
	v := baseView("s1", "pw")
	v.ExpiresAt = fixedNow.Add(-time.Hour)
	v.ExpiryBehavior = "block"
	env.provider.views["u"] = v
	env.addSession("u", "s1")

	env.le.tick(context.Background())

	if len(env.tw.coa.disconnects)+len(env.tw.coa.rates)+len(env.tw.coa.pools) != 0 {
		t.Fatalf("expiry crossing must not be acted on by the limits worker: coa=%+v", env.tw.coa)
	}
}

func TestLimitsWorker_DeferredToEnforceGoForQuota(t *testing.T) {
	env := newLimitsTestEnv(t, fixedNow)
	v := baseView("s1", "pw")
	v.QuotaExhausted = true
	v.QuotaBehavior = "throttle"
	v.ThrottleRate = "1M/1M"
	env.provider.views["u"] = v
	env.addSession("u", "s1")

	env.le.tick(context.Background())

	if len(env.tw.coa.disconnects)+len(env.tw.coa.rates)+len(env.tw.coa.pools) != 0 {
		t.Fatalf("quota crossing must not be acted on by the limits worker: coa=%+v", env.tw.coa)
	}
}

func TestLimitsWorker_OfflineOrUnknownIsNoOp(t *testing.T) {
	env := newLimitsTestEnv(t, fixedNow)
	// No sessions at all.
	env.le.tick(context.Background())
	if len(env.tw.records) != 0 {
		t.Fatalf("expected no records with no online sessions, got %+v", env.tw.records)
	}

	// A session for a username the provider doesn't know.
	env.addSession("ghost", "s1")
	env.le.tick(context.Background())
	if len(env.tw.records) != 0 {
		t.Fatalf("expected no records for an unknown subscriber, got %+v", env.tw.records)
	}
}

// --- speed windows: entry transition + throttle exclusion -----------------

func TestLimitsWorker_WindowEntry_FiresOnlyOnTransition(t *testing.T) {
	env := newLimitsTestEnv(t, fixedNow)
	v := baseView("s1", "pw")
	env.provider.views["u"] = v
	env.addSession("u", "s1")

	// First tick: no window active — establishes baseline, must not fire.
	env.le.tick(context.Background())
	if len(env.tw.coa.rates) != 0 {
		t.Fatalf("baseline tick must not fire a rate sync, got %+v", env.tw.coa.rates)
	}

	// Window becomes active: second tick must sync to the boosted rate.
	local := fixedNow.In(serverLocation())
	mod := local.Hour()*60 + local.Minute()
	v.SpeedWindows = []SpeedWindow{{StartMin: mod, EndMin: (mod + 60) % 1440, Rate: "80M/80M"}}
	env.provider.views["u"] = v
	env.le.tick(context.Background())
	if len(env.tw.coa.rates) != 1 || env.tw.coa.rates[0].rate != "80M/80M" {
		t.Fatalf("expected one rate sync to the window rate, got %+v", env.tw.coa.rates)
	}

	// Steady state: a third identical tick must not re-fire.
	env.le.tick(context.Background())
	if len(env.tw.coa.rates) != 1 {
		t.Fatalf("steady-state tick must not re-fire, got %+v", env.tw.coa.rates)
	}
}

func TestLimitsWorker_RestoreAfterUptimeClearsAndDailyReset(t *testing.T) {
	env := newLimitsTestEnv(t, fixedNow)
	v := baseView("s1", "pw")
	v.UptimeLimitSeconds, v.UptimeUsedSeconds = 100, 200
	v.UptimeExhaustAction = "throttle"
	v.UptimeThrottleRate = "1M/1M"
	env.provider.views["u"] = v
	env.addSession("u", "s1")

	// Tick 1: uptime exceeded, throttled.
	env.le.tick(context.Background())
	if len(env.tw.coa.rates) != 1 || env.tw.coa.rates[0].rate != "1M/1M" {
		t.Fatalf("expected throttle rate applied, got %+v", env.tw.coa.rates)
	}

	// The limit clears (e.g. renewal bumped the anchor forward). Tick 2 must
	// actively restore full speed, not silently treat it as a fresh baseline.
	v.UptimeUsedSeconds = 0
	env.provider.views["u"] = v
	env.le.tick(context.Background())
	if len(env.tw.coa.rates) != 2 || env.tw.coa.rates[1].rate != "10M/10M" {
		t.Fatalf("expected a forced restore to the base rate, got %+v", env.tw.coa.rates)
	}
}

// TestLimitsWorker_DailyResetRestoresThrottledSubscriberSameTick is gate item
// 4's combined proof: a subscriber throttled by the daily limit is restored
// to full speed within the SAME tick the reset fires, with no operator
// action — dailyResetTick runs before the per-session evaluation loop, and
// the fake resetDailyCounters mimics the real one's effect (advancing the
// period start, which is what makes a fresh evaluation read as under-limit).
func TestLimitsWorker_DailyResetRestoresThrottledSubscriberSameTick(t *testing.T) {
	env := newLimitsTestEnv(t, fixedNow)
	v := baseView("s1", "pw")
	v.DailyLimitBytes, v.DailyUsedBytes = 1000, 1000
	v.DailyExhaustAction = "throttle"
	v.DailyThrottleRate = "1M/1M"
	env.provider.views["u"] = v
	env.addSession("u", "s1")

	// Tick 1: daily exceeded, throttled — no reset configured yet.
	env.le.tick(context.Background())
	if len(env.tw.coa.rates) != 1 || env.tw.coa.rates[0].rate != "1M/1M" {
		t.Fatalf("expected daily throttle rate applied, got %+v", env.tw.coa.rates)
	}

	// Configure the profile's reset as due, and have the fake
	// resetDailyCounters clear the subscriber's usage (mirroring what
	// advancing daily_period_start does to a fresh AuthView load).
	local := fixedNow.In(serverLocation())
	mod := local.Hour()*60 + local.Minute()
	profileID := "22222222-2222-2222-2222-222222222222"
	env.resetPro = []dailyResetProfile{{ID: profileID, ResetMin: mod - 1}}
	env.le.resetDailyCounters = func(_ context.Context, pID string) ([]string, error) {
		env.resetCalls = append(env.resetCalls, pID)
		v.DailyUsedBytes = 0
		env.provider.views["u"] = v
		return []string{"s1"}, nil
	}

	// Tick 2: reset fires, then the same tick re-evaluates the session and
	// must actively restore full speed.
	env.le.tick(context.Background())
	if len(env.resetCalls) != 1 {
		t.Fatalf("expected the reset to fire, got %d calls", len(env.resetCalls))
	}
	if len(env.tw.coa.rates) != 2 || env.tw.coa.rates[1].rate != "10M/10M" {
		t.Fatalf("expected a same-tick restore to the base rate, got %+v", env.tw.coa.rates)
	}
}

// --- daily reset sweep ------------------------------------------------------

func TestLimitsWorker_DailyResetSweep_ClaimsOncePerDay(t *testing.T) {
	env := newLimitsTestEnv(t, fixedNow) // fixedNow = 2026-07-10 12:00 UTC
	local := fixedNow.In(serverLocation())
	mod := local.Hour()*60 + local.Minute()
	env.resetPro = []dailyResetProfile{{ID: "11111111-1111-1111-1111-111111111111", ResetMin: mod - 1}}
	env.resetSubs["11111111-1111-1111-1111-111111111111"] = []string{"s1", "s2"}

	env.le.tick(context.Background())
	env.le.tick(context.Background()) // same day: must not reset twice

	if len(env.resetCalls) != 1 {
		t.Fatalf("expected exactly one reset call, got %d: %v", len(env.resetCalls), env.resetCalls)
	}
	if len(env.audits) != 1 || env.audits[0] != "enforce.daily_reset:11111111-1111-1111-1111-111111111111" {
		t.Fatalf("expected one daily_reset audit entry, got %v", env.audits)
	}
}

func TestLimitsWorker_DailyResetSweep_NotYetDueSkipped(t *testing.T) {
	env := newLimitsTestEnv(t, fixedNow)
	local := fixedNow.In(serverLocation())
	mod := local.Hour()*60 + local.Minute()
	env.resetPro = []dailyResetProfile{{ID: "11111111-1111-1111-1111-111111111111", ResetMin: mod + 5}}

	env.le.tick(context.Background())

	if len(env.resetCalls) != 0 {
		t.Fatalf("expected no reset before the configured minute, got %v", env.resetCalls)
	}
}

// --- pure function: planStepsForOutcome ------------------------------------

func TestPlanStepsForOutcome_Matrix(t *testing.T) {
	view := AuthView{RateLimit: "10M/10M", ThrottleRate: "5M/5M", ExpiredPoolName: "global-expired"}
	cases := []struct {
		name    string
		oc      policyOutcome
		wantOps []string
		wantLbl string
	}{
		{"reject", policyOutcome{Reject: true}, []string{"disconnect"}, "block"},
		{"throttle_own_rate", policyOutcome{Mode: modeThrottled, Rate: "1M/1M"}, []string{"apply_rate"}, "throttle"},
		{"throttle_falls_back_to_base", policyOutcome{Mode: modeThrottled}, []string{"apply_rate"}, "throttle"},
		{"expired_pool_own_target", policyOutcome{Mode: modeExpiredPool, Pool: "own-pool", Rate: "2M/2M"}, []string{"move_pool", "apply_rate"}, "expired_pool"},
		{"expired_pool_falls_back", policyOutcome{Mode: modeExpiredPool}, []string{"move_pool", "apply_rate"}, "expired_pool"},
		{"switch_plan_with_pool", policyOutcome{Mode: modeSwitchPlan, Rate: "3M/3M", Pool: "target-pool"}, []string{"move_pool", "apply_rate"}, "switch_plan"},
		{"switch_plan_no_pool", policyOutcome{Mode: modeSwitchPlan, Rate: "3M/3M"}, []string{"apply_rate"}, "switch_plan"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			steps, label := planStepsForOutcome(view, c.oc)
			if label != c.wantLbl {
				t.Errorf("label = %q, want %q", label, c.wantLbl)
			}
			if len(steps) != len(c.wantOps) {
				t.Fatalf("steps = %+v, want ops %v", steps, c.wantOps)
			}
			for i, op := range c.wantOps {
				if steps[i].op != op {
					t.Errorf("step %d op = %q, want %q", i, steps[i].op, op)
				}
			}
		})
	}
}
