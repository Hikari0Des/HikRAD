package radius

// Limits engine (v4-4, FR-128–131). This file is the ONE implementation the
// phase brief's frozen evaluation order requires
// (docs/v4/phases/phase-v4-4-limits-engine/00-phase.md §4): expiry → quota →
// uptime → daily → speed windows, with an identical unified exhaustion
// action set (block | throttle | switch_plan | expired_pool) for all four
// limits. policy.go's decide() (auth time) and limitsworker.go's continuous
// sweep (already-online sessions) both call evaluateLimits/selectSpeedWindow
// directly — two call sites, one decision function, so they cannot drift.
//
// Speed windows (FR-131) are a NEW, separate mechanism from the older FR-11
// profile_tod_windows (tod.go): the old TOD windows apply regardless of
// throttle state and have no day-of-week control; speed windows are
// EXCLUDED for a subscriber a limit is currently throttling (a window must
// never undo an enforcement action) and support day-of-week + "highest
// speed wins" among overlapping matches. Deliberately not merged — gate item
// 12 requires tod.go and its tests untouched, and the two features answer
// different product questions (a courtesy speed boost vs. a plan ceiling).

import (
	"strconv"
	"strings"
	"time"
)

// limitAction is one limit's resolved non-reject outcome.
type limitAction struct {
	Mode outcomeMode
	Rate string
	Pool string
}

// resolveAction is the unified FR-130 action resolver, shared verbatim by
// expiry, quota, uptime and daily. reject=true for "block" and for any
// unrecognized action — fail-safe, so a misconfigured or pre-migration-empty
// action can never silently grant full access to an exhausted limit (the
// pre-v4-4 expiry check already had exactly this fail-safe-reject posture;
// FR-130 extends it uniformly rather than relaxing it).
func resolveAction(action, throttleRate, poolOverride, switchRate, switchPool string) (reject bool, la limitAction) {
	switch action {
	case "throttle":
		return false, limitAction{Mode: modeThrottled, Rate: throttleRate}
	case "expired_pool":
		return false, limitAction{Mode: modeExpiredPool, Pool: poolOverride}
	case "switch_plan":
		return false, limitAction{Mode: modeSwitchPlan, Rate: switchRate, Pool: switchPool}
	default: // "block", "", or anything unrecognized
		return true, limitAction{}
	}
}

// policyOutcome is the full result of evaluating one subscriber's limits —
// reused verbatim by the worker so auth time and continuous enforcement
// share one decision.
type policyOutcome struct {
	Reject bool
	Reason string
	Mode   outcomeMode
	Rate   string
	Pool   string
	// Kind names which limit produced this outcome: "expiry" | "quota" |
	// "uptime" | "daily" | "" (modeNormal, nothing exhausted). The continuous
	// worker (limitsworker.go) uses it to act ONLY on uptime/daily — expiry
	// and quota's mid-session enforcement already belongs to the existing
	// enforce.go pub/sub worker (untouched by this phase), and having two
	// workers both CoA the same crossing is exactly the "two paths that can
	// drift" failure mode the phase brief calls out.
	Kind string
}

// evaluateLimits runs the frozen order (§4). The FIRST exhausted limit's
// action wins outright; later limits in the order are not consulted — the
// same rule the pre-v4-4 expiry/quota if/else-if chain already followed
// (only quota's `else if` ran when expiry had not already set a mode),
// extended uniformly to all four. hotspot / quotaExempt mirror decide()'s
// own FR-58.3 exemption exactly, since evaluateLimits replaces that step's
// body.
func evaluateLimits(view AuthView, hotspot bool, isExpired bool) policyOutcome {
	switch {
	case isExpired:
		reject, la := resolveAction(view.ExpiryBehavior, view.ExpiryThrottleRate, view.ExpiryPoolOverride, view.ExpirySwitchRate, view.ExpirySwitchPool)
		if reject {
			return policyOutcome{Reject: true, Reason: ReasonExpired, Kind: "expiry"}
		}
		return policyOutcome{Mode: la.Mode, Rate: la.Rate, Pool: la.Pool, Kind: "expiry"}

	case !quotaExempt(view.ServiceType, hotspot) && view.QuotaExhausted:
		reject, la := resolveAction(view.QuotaBehavior, view.ThrottleRate, view.QuotaPoolOverride, view.QuotaSwitchRate, view.QuotaSwitchPool)
		if reject {
			return policyOutcome{Reject: true, Reason: ReasonQuotaExhausted, Kind: "quota"}
		}
		return policyOutcome{Mode: la.Mode, Rate: la.Rate, Pool: la.Pool, Kind: "quota"}

	case view.UptimeLimitSeconds > 0 && view.UptimeUsedSeconds >= view.UptimeLimitSeconds:
		reject, la := resolveAction(view.UptimeExhaustAction, view.UptimeThrottleRate, view.UptimePoolOverride, view.UptimeSwitchRate, view.UptimeSwitchPool)
		if reject {
			return policyOutcome{Reject: true, Reason: ReasonUptimeExhausted, Kind: "uptime"}
		}
		return policyOutcome{Mode: la.Mode, Rate: la.Rate, Pool: la.Pool, Kind: "uptime"}

	case view.DailyLimitBytes > 0 && view.DailyUsedBytes >= view.DailyLimitBytes:
		action := view.DailyExhaustAction
		if action == "" {
			// FR-129: throttle is the default action; the DB column defaults
			// it too (0597), this only guards a view built before the column
			// existed (mirrors ExpiryBehavior/QuotaBehavior's strOr default
			// in subscribers/authview.go).
			action = "throttle"
		}
		reject, la := resolveAction(action, view.DailyThrottleRate, view.DailyPoolOverride, view.DailySwitchRate, view.DailySwitchPool)
		if reject {
			return policyOutcome{Reject: true, Reason: ReasonDailyExhausted, Kind: "daily"}
		}
		return policyOutcome{Mode: la.Mode, Rate: la.Rate, Pool: la.Pool, Kind: "daily"}

	default:
		return policyOutcome{Mode: modeNormal}
	}
}

// --- Rate/pool resolution shared by policy.go (auth time) and
// limitsworker.go (continuous enforcement) — the same "ONE implementation"
// discipline evaluateLimits itself follows, extended to what the reply/CoA
// actually carries.

// normalRate resolves the FULL-SPEED (modeNormal) rate: the profile/hotspot
// base rate, a currently-active speed window override (FR-131) if any, then
// burst composition (FR-11).
func normalRate(view AuthView, hotspot bool, windowRate string) string {
	rate := view.RateLimit
	if hotspot && view.HotspotRateLimit != "" {
		rate = view.HotspotRateLimit
	}
	if windowRate != "" {
		rate = windowRate
	}
	return composeRate(view, rate)
}

// throttleOrSwitchRate resolves the modeThrottled/modeSwitchPlan rate: the
// exhausted limit's own configured rate, falling back to the full base rate
// when unset (matches the pre-v4-4 quota-throttle fallback exactly).
func throttleOrSwitchRate(view AuthView, oc policyOutcome) string {
	if oc.Rate != "" {
		return oc.Rate
	}
	return view.RateLimit
}

// expiredPoolRateAndPool resolves the modeExpiredPool reply: the exhausted
// limit's own pool override else the shared global expired pool, and its own
// rate else the shared quota throttle rate else the walled-garden minimum.
func expiredPoolRateAndPool(view AuthView, oc policyOutcome) (rate, pool string) {
	pool = oc.Pool
	if pool == "" {
		pool = view.ExpiredPoolName
	}
	rate = oc.Rate
	if rate == "" {
		rate = view.ThrottleRate
	}
	if rate == "" {
		rate = expiredPoolFallbackRate
	}
	return rate, pool
}

// limitCheckLabel names the FR-39 debug-tail entry for a non-normal outcome.
func limitCheckLabel(mode outcomeMode) string {
	switch mode {
	case modeThrottled:
		return "throttled"
	case modeExpiredPool:
		return "expired_pool"
	case modeSwitchPlan:
		return "switch_plan"
	default:
		return ""
	}
}

// --- Speed windows (FR-131) -------------------------------------------------

// SpeedWindow is one profile's time-of-day rate override. Start/End are
// minutes since local (server-timezone) midnight; a window with End < Start
// wraps past midnight, reusing tod.go's windowActive rather than
// reimplementing the wrap math. DaysMask bit N set = day N included
// (0=Sunday..6=Saturday, matching time.Weekday so no remapping table is
// needed); 0 = every day (empty set = unrestricted, the same convention
// profile/subscriber NAS scopes already use).
type SpeedWindow struct {
	ID        string
	ProfileID string
	Label     string
	DaysMask  int
	StartMin  int
	EndMin    int
	Rate      string // abstract "rx/tx" intent, composed by the loader
}

// serverLocation is the timezone speed windows and the daily-reset sweep are
// evaluated in — Asia/Baghdad, matching the existing FR-11 TOD sweep's
// convention (tod.go's startTODSweeps) so an operator configuring either
// kind of window uses the same mental model. Falls back to the fixed UTC+3
// offset exactly as tod.go already does when tzdata is missing (Iraq
// observes no DST, so the fallback is exact, not approximate).
func serverLocation() *time.Location {
	if loc, err := time.LoadLocation("Asia/Baghdad"); err == nil {
		return loc
	}
	return time.FixedZone("Asia/Baghdad", 3*60*60)
}

// speedWindowActive reports whether t (already in server-local time) falls
// inside w, on both the day-of-week mask and the minute-of-day range.
func speedWindowActive(w SpeedWindow, t time.Time) bool {
	if w.DaysMask != 0 && w.DaysMask&(1<<uint(t.Weekday())) == 0 {
		return false
	}
	return windowActive(TODWindow{StartMin: w.StartMin, EndMin: w.EndMin}, minuteOfDay(t))
}

// selectSpeedWindow picks the currently-active window with the highest
// combined rate (FR-131: "highest speed wins" when several match). t must
// already be in server-local time (serverLocation()).
func selectSpeedWindow(windows []SpeedWindow, t time.Time) (SpeedWindow, bool) {
	var best SpeedWindow
	found := false
	for _, w := range windows {
		if !speedWindowActive(w, t) {
			continue
		}
		if !found || rateMagnitude(w.Rate) > rateMagnitude(best.Rate) {
			best, found = w, true
		}
	}
	return best, found
}

// rateMagnitude scores an abstract "rx/tx" rate string (sum of both sides in
// kbps) for the highest-wins comparison. Malformed segments parse as 0 so a
// bad rate loses the comparison instead of erroring.
func rateMagnitude(rate string) int {
	total := 0
	for _, p := range strings.SplitN(rate, "/", 2) {
		total += parseRateKbps(p)
	}
	return total
}

// parseRateKbps inverts subscribers/authview.go's rateToken: "<n>M" = n*1024
// kbps, "<n>k" or a bare number = n kbps.
func parseRateKbps(tok string) int {
	tok = strings.TrimSpace(tok)
	if tok == "" {
		return 0
	}
	mult := 1
	switch {
	case strings.HasSuffix(tok, "M"):
		mult, tok = 1024, strings.TrimSuffix(tok, "M")
	case strings.HasSuffix(tok, "k"):
		tok = strings.TrimSuffix(tok, "k")
	}
	n, err := strconv.Atoi(tok)
	if err != nil {
		return 0
	}
	return n * mult
}
