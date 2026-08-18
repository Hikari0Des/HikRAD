package radius

// Unit tests for the v4-4 Limits engine (FR-128–131): uptime, daily, the
// unified exhaustion action set (including the new throttle-for-expiry and
// switch_plan actions), the frozen evaluation order, and speed windows.
// Reuses authorize_test.go's harness (newTestEnv/baseView/mustDecide/etc.) —
// same package, same fixtures, so these are proven against the identical
// engine the pre-v4-4 suite runs against.

import (
	"testing"
	"time"
)

// --- FR-128 uptime -----------------------------------------------------

func TestUptimeBlock(t *testing.T) {
	env := newTestEnv(t, "10.0.0.1")
	v := baseView("s1", "pw")
	v.UptimeLimitSeconds = 3600
	v.UptimeUsedSeconds = 3600
	v.UptimeExhaustAction = "block"
	env.add("u", v)
	assertReject(t, mustDecide(t, env, papReq("u", "pw")), ReasonUptimeExhausted)
}

func TestUptimeThrottle(t *testing.T) {
	env := newTestEnv(t, "10.0.0.1")
	v := baseView("s1", "pw")
	v.UptimeLimitSeconds = 3600
	v.UptimeUsedSeconds = 3601
	v.UptimeExhaustAction = "throttle"
	v.UptimeThrottleRate = "1M/1M"
	env.add("u", v)
	resp := mustDecide(t, env, papReq("u", "pw"))
	assertAccept(t, resp)
	if attrMap(resp)[string(IntentRateLimit)] != "1M/1M" {
		t.Fatalf("expected uptime throttle rate, got %+v", resp.Attributes)
	}
}

func TestUptimeExpiredPool(t *testing.T) {
	env := newTestEnv(t, "10.0.0.1")
	v := baseView("s1", "pw")
	v.UptimeLimitSeconds = 3600
	v.UptimeUsedSeconds = 3600
	v.UptimeExhaustAction = "expired_pool"
	v.ExpiredPoolName = "walled" // shared global fallback pool
	env.add("u", v)
	resp := mustDecide(t, env, papReq("u", "pw"))
	assertAccept(t, resp)
	a := attrMap(resp)
	if a[string(IntentAddressPool)] != "walled" {
		t.Fatalf("expected walled-garden pool, got %+v", resp.Attributes)
	}
	if a[string(IntentRedirectExpired)] == "" {
		t.Fatalf("expected redirect_expired intent, got %+v", resp.Attributes)
	}
}

func TestUptimeSwitchPlan(t *testing.T) {
	env := newTestEnv(t, "10.0.0.1")
	v := baseView("s1", "pw")
	v.UptimeLimitSeconds = 3600
	v.UptimeUsedSeconds = 3600
	v.UptimeExhaustAction = "switch_plan"
	v.UptimeSwitchRate = "512k/512k"
	v.UptimeSwitchPool = "downgraded"
	env.add("u", v)
	resp := mustDecide(t, env, papReq("u", "pw"))
	assertAccept(t, resp)
	a := attrMap(resp)
	if a[string(IntentRateLimit)] != "512k/512k" {
		t.Fatalf("expected switch-plan rate, got %+v", resp.Attributes)
	}
	if a[string(IntentAddressPool)] != "downgraded" {
		t.Fatalf("expected switch-plan pool, got %+v", resp.Attributes)
	}
}

func TestUptimeNotYetExhaustedAccepts(t *testing.T) {
	env := newTestEnv(t, "10.0.0.1")
	v := baseView("s1", "pw")
	v.UptimeLimitSeconds = 3600
	v.UptimeUsedSeconds = 100
	v.UptimeExhaustAction = "block"
	env.add("u", v)
	resp := mustDecide(t, env, papReq("u", "pw"))
	assertAccept(t, resp)
	if attrMap(resp)[string(IntentRateLimit)] != "10M/10M" {
		t.Fatalf("expected normal rate, got %+v", resp.Attributes)
	}
}

// --- FR-129 daily (default action is throttle, not block) --------------

func TestDailyThrottleDefaultAction(t *testing.T) {
	env := newTestEnv(t, "10.0.0.1")
	v := baseView("s1", "pw")
	v.DailyLimitBytes = 1_000_000_000
	v.DailyUsedBytes = 1_000_000_001
	// DailyExhaustAction deliberately left unset: the zero value must still
	// throttle, not silently accept at full speed and not reject — FR-129's
	// commercial point.
	v.DailyThrottleRate = "2M/2M"
	env.add("u", v)
	resp := mustDecide(t, env, papReq("u", "pw"))
	assertAccept(t, resp)
	if attrMap(resp)[string(IntentRateLimit)] != "2M/2M" {
		t.Fatalf("expected daily default-throttle rate, got %+v", resp.Attributes)
	}
}

func TestDailyBlock(t *testing.T) {
	env := newTestEnv(t, "10.0.0.1")
	v := baseView("s1", "pw")
	v.DailyLimitBytes = 1_000_000_000
	v.DailyUsedBytes = 1_000_000_000
	v.DailyExhaustAction = "block"
	env.add("u", v)
	assertReject(t, mustDecide(t, env, papReq("u", "pw")), ReasonDailyExhausted)
}

func TestDailyBelowLimitAccepted(t *testing.T) {
	env := newTestEnv(t, "10.0.0.1")
	v := baseView("s1", "pw")
	v.DailyLimitBytes = 1_000_000_000
	v.DailyUsedBytes = 500_000_000
	v.DailyExhaustAction = "block" // would reject if (wrongly) evaluated as exhausted
	env.add("u", v)
	assertAccept(t, mustDecide(t, env, papReq("u", "pw")))
}

// --- FR-130 unified action set on the pre-existing limits ---------------

func TestExpiryThrottle(t *testing.T) {
	env := newTestEnv(t, "10.0.0.1")
	v := baseView("s1", "pw")
	v.ExpiresAt = fixedNow.Add(-time.Hour)
	v.ExpiryBehavior = "throttle"
	v.ExpiryThrottleRate = "1M/1M"
	env.add("u", v)
	resp := mustDecide(t, env, papReq("u", "pw"))
	assertAccept(t, resp)
	if attrMap(resp)[string(IntentRateLimit)] != "1M/1M" {
		t.Fatalf("expected expiry throttle rate, got %+v", resp.Attributes)
	}
}

func TestExpirySwitchPlan(t *testing.T) {
	env := newTestEnv(t, "10.0.0.1")
	v := baseView("s1", "pw")
	v.ExpiresAt = fixedNow.Add(-time.Hour)
	v.ExpiryBehavior = "switch_plan"
	v.ExpirySwitchRate = "3M/3M"
	v.ExpirySwitchPool = "renewal-offer"
	env.add("u", v)
	resp := mustDecide(t, env, papReq("u", "pw"))
	assertAccept(t, resp)
	a := attrMap(resp)
	if a[string(IntentRateLimit)] != "3M/3M" || a[string(IntentAddressPool)] != "renewal-offer" {
		t.Fatalf("expected switch-plan attrs, got %+v", resp.Attributes)
	}
}

func TestQuotaSwitchPlan(t *testing.T) {
	env := newTestEnv(t, "10.0.0.1")
	v := baseView("s1", "pw")
	v.QuotaExhausted = true
	v.QuotaBehavior = "switch_plan"
	v.QuotaSwitchRate = "4M/4M"
	v.QuotaSwitchPool = "topup-offer"
	env.add("u", v)
	resp := mustDecide(t, env, papReq("u", "pw"))
	assertAccept(t, resp)
	a := attrMap(resp)
	if a[string(IntentRateLimit)] != "4M/4M" || a[string(IntentAddressPool)] != "topup-offer" {
		t.Fatalf("expected switch-plan attrs, got %+v", resp.Attributes)
	}
}

// --- Frozen evaluation order (§4): expiry > quota > uptime > daily ------

func TestFrozenOrder_ExpiryWinsOverEverything(t *testing.T) {
	env := newTestEnv(t, "10.0.0.1")
	v := baseView("s1", "pw")
	v.ExpiresAt = fixedNow.Add(-time.Hour)
	v.ExpiryBehavior = "block"
	v.QuotaExhausted = true
	v.QuotaBehavior = "throttle"
	v.ThrottleRate = "9M/9M" // also exhausted, must not apply
	v.UptimeLimitSeconds, v.UptimeUsedSeconds = 1, 1
	v.DailyLimitBytes, v.DailyUsedBytes = 1, 1
	env.add("u", v)
	// Expiry's block wins outright: rejects with "expired", never reaching
	// quota/uptime/daily.
	assertReject(t, mustDecide(t, env, papReq("u", "pw")), ReasonExpired)
}

func TestFrozenOrder_QuotaWinsOverUptimeAndDaily(t *testing.T) {
	env := newTestEnv(t, "10.0.0.1")
	v := baseView("s1", "pw")
	v.QuotaExhausted = true
	v.QuotaBehavior = "throttle"
	v.ThrottleRate = "1M/1M"
	v.UptimeLimitSeconds, v.UptimeUsedSeconds = 10, 20
	v.UptimeExhaustAction = "block" // would reject if consulted
	v.DailyLimitBytes, v.DailyUsedBytes = 10, 20
	v.DailyExhaustAction = "block" // would reject if consulted
	env.add("u", v)
	resp := mustDecide(t, env, papReq("u", "pw"))
	assertAccept(t, resp) // quota's throttle wins, not uptime/daily's block
	if attrMap(resp)[string(IntentRateLimit)] != "1M/1M" {
		t.Fatalf("expected quota throttle rate, got %+v", resp.Attributes)
	}
}

func TestFrozenOrder_UptimeWinsOverDaily(t *testing.T) {
	env := newTestEnv(t, "10.0.0.1")
	v := baseView("s1", "pw")
	v.UptimeLimitSeconds, v.UptimeUsedSeconds = 10, 20
	v.UptimeExhaustAction = "throttle"
	v.UptimeThrottleRate = "1M/1M"
	v.DailyLimitBytes, v.DailyUsedBytes = 10, 20
	v.DailyExhaustAction = "block" // would reject if consulted
	env.add("u", v)
	resp := mustDecide(t, env, papReq("u", "pw"))
	assertAccept(t, resp)
	if attrMap(resp)[string(IntentRateLimit)] != "1M/1M" {
		t.Fatalf("expected uptime throttle rate, got %+v", resp.Attributes)
	}
}

// --- FR-131 speed windows -------------------------------------------------

// localFixedNow is fixedNow (authorize_test.go) converted to server-local
// time, the same conversion decide() performs for window resolution.
func localFixedNow() time.Time { return fixedNow.In(serverLocation()) }

func TestSpeedWindow_AppliesAtNormalRate(t *testing.T) {
	env := newTestEnv(t, "10.0.0.1")
	v := baseView("s1", "pw")
	local := localFixedNow()
	mod := local.Hour()*60 + local.Minute()
	v.SpeedWindows = []SpeedWindow{
		{StartMin: mod, EndMin: (mod + 60) % 1440, Rate: "50M/50M"},
	}
	env.add("u", v)
	resp := mustDecide(t, env, papReq("u", "pw"))
	assertAccept(t, resp)
	if attrMap(resp)[string(IntentRateLimit)] != "50M/50M" {
		t.Fatalf("expected window rate, got %+v", resp.Attributes)
	}
}

func TestSpeedWindow_InactiveWindowIgnored(t *testing.T) {
	env := newTestEnv(t, "10.0.0.1")
	v := baseView("s1", "pw")
	local := localFixedNow()
	mod := local.Hour()*60 + local.Minute()
	// A window that ended a minute ago and won't start again for a day.
	start := (mod - 120 + 1440) % 1440
	end := (mod - 60 + 1440) % 1440
	v.SpeedWindows = []SpeedWindow{{StartMin: start, EndMin: end, Rate: "50M/50M"}}
	env.add("u", v)
	resp := mustDecide(t, env, papReq("u", "pw"))
	assertAccept(t, resp)
	if attrMap(resp)[string(IntentRateLimit)] != "10M/10M" {
		t.Fatalf("expected base rate (window inactive), got %+v", resp.Attributes)
	}
}

func TestSpeedWindow_HighestWins(t *testing.T) {
	env := newTestEnv(t, "10.0.0.1")
	v := baseView("s1", "pw")
	local := localFixedNow()
	mod := local.Hour()*60 + local.Minute()
	v.SpeedWindows = []SpeedWindow{
		{StartMin: mod, EndMin: (mod + 60) % 1440, Rate: "20M/20M"},
		{StartMin: mod, EndMin: (mod + 60) % 1440, Rate: "80M/80M"}, // overlapping, faster
		{StartMin: mod, EndMin: (mod + 60) % 1440, Rate: "5M/5M"},
	}
	env.add("u", v)
	resp := mustDecide(t, env, papReq("u", "pw"))
	assertAccept(t, resp)
	if attrMap(resp)[string(IntentRateLimit)] != "80M/80M" {
		t.Fatalf("expected the fastest overlapping window, got %+v", resp.Attributes)
	}
}

func TestSpeedWindow_DayOfWeekExcludesToday(t *testing.T) {
	env := newTestEnv(t, "10.0.0.1")
	v := baseView("s1", "pw")
	local := localFixedNow()
	mod := local.Hour()*60 + local.Minute()
	// Mask excludes today's weekday specifically (every OTHER day included).
	todayBit := 1 << uint(local.Weekday())
	mask := 127 &^ todayBit
	v.SpeedWindows = []SpeedWindow{
		{StartMin: mod, EndMin: (mod + 60) % 1440, Rate: "80M/80M", DaysMask: mask},
	}
	env.add("u", v)
	resp := mustDecide(t, env, papReq("u", "pw"))
	assertAccept(t, resp)
	if attrMap(resp)[string(IntentRateLimit)] != "10M/10M" {
		t.Fatalf("expected base rate (today excluded by mask), got %+v", resp.Attributes)
	}
}

func TestSpeedWindow_MidnightCrossing(t *testing.T) {
	env := newTestEnv(t, "10.0.0.1")
	v := baseView("s1", "pw")
	local := localFixedNow()
	mod := local.Hour()*60 + local.Minute()
	// A window that started an hour ago and wraps past midnight to an hour
	// from now — active regardless of which side of midnight "now" is on.
	start := (mod - 60 + 1440) % 1440
	end := (mod + 60) % 1440
	v.SpeedWindows = []SpeedWindow{{StartMin: start, EndMin: end, Rate: "80M/80M"}}
	env.add("u", v)
	resp := mustDecide(t, env, papReq("u", "pw"))
	assertAccept(t, resp)
	if attrMap(resp)[string(IntentRateLimit)] != "80M/80M" {
		t.Fatalf("expected window rate across the midnight-style wrap, got %+v", resp.Attributes)
	}
}

// TestSpeedWindow_ExcludedWhenThrottled is gate item 7: a window must never
// undo an enforcement action.
func TestSpeedWindow_ExcludedWhenThrottled(t *testing.T) {
	env := newTestEnv(t, "10.0.0.1")
	v := baseView("s1", "pw")
	v.QuotaExhausted = true
	v.QuotaBehavior = "throttle"
	v.ThrottleRate = "1M/1M"
	local := localFixedNow()
	mod := local.Hour()*60 + local.Minute()
	v.SpeedWindows = []SpeedWindow{{StartMin: mod, EndMin: (mod + 60) % 1440, Rate: "80M/80M"}}
	env.add("u", v)
	resp := mustDecide(t, env, papReq("u", "pw"))
	assertAccept(t, resp)
	if got := attrMap(resp)[string(IntentRateLimit)]; got != "1M/1M" {
		t.Fatalf("window must not override the active throttle: got rate %q", got)
	}
}

// --- pure-function coverage ------------------------------------------------

func TestResolveAction(t *testing.T) {
	cases := []struct {
		action     string
		wantReject bool
		wantMode   outcomeMode
	}{
		{"block", true, modeNormal},
		{"", true, modeNormal},
		{"garbage", true, modeNormal},
		{"throttle", false, modeThrottled},
		{"expired_pool", false, modeExpiredPool},
		{"switch_plan", false, modeSwitchPlan},
	}
	for _, c := range cases {
		reject, la := resolveAction(c.action, "", "", "", "")
		if reject != c.wantReject {
			t.Errorf("action %q: reject = %v, want %v", c.action, reject, c.wantReject)
		}
		if !reject && la.Mode != c.wantMode {
			t.Errorf("action %q: mode = %v, want %v", c.action, la.Mode, c.wantMode)
		}
	}
}

func TestRateMagnitude(t *testing.T) {
	cases := []struct {
		rate string
		want int
	}{
		{"10M/10M", 20480},
		{"512k/512k", 1024},
		{"1M/512k", 1536},
		{"", 0},
		{"garbage/garbage", 0},
	}
	for _, c := range cases {
		if got := rateMagnitude(c.rate); got != c.want {
			t.Errorf("rateMagnitude(%q) = %d, want %d", c.rate, got, c.want)
		}
	}
}
