package profiles

// Pure unit tests for profileInput.validate() — no DB needed. Covers the
// v4-4 Limits engine additions (FR-128–131): the widened expiry_behavior/
// quota_behavior enums, and the new uptime/daily validation.

import (
	"testing"

	"github.com/hikrad/hikrad/internal/httpapi"
)

func baseValidInput() profileInput {
	return profileInput{
		Name: "Test Plan", Price: 1000, DurationDays: 30,
		RateDownKbps: 1024, RateUpKbps: 1024, SessionLimitDefault: 1,
	}
}

func hasFieldError(fe []httpapi.FieldError, field string) bool {
	for _, e := range fe {
		if e.Field == field {
			return true
		}
	}
	return false
}

func TestValidate_DefaultsAreValid(t *testing.T) {
	in := baseValidInput()
	if fe := in.validate(); len(fe) != 0 {
		t.Fatalf("expected no errors on defaults, got %+v", fe)
	}
	if in.ExpiryBehavior != "block" || in.QuotaBehavior != "block" {
		t.Fatalf("expected block defaults, got expiry=%q quota=%q", in.ExpiryBehavior, in.QuotaBehavior)
	}
	if in.UptimeExhaustAction != "block" {
		t.Fatalf("expected uptime default block, got %q", in.UptimeExhaustAction)
	}
	if in.DailyExhaustAction != "throttle" {
		t.Fatalf("expected daily default throttle (FR-129), got %q", in.DailyExhaustAction)
	}
}

func TestValidate_ExpiryWidenedActions(t *testing.T) {
	for _, action := range []string{"block", "expired_pool", "throttle", "switch_plan"} {
		in := baseValidInput()
		in.ExpiryBehavior = action
		if action == "throttle" {
			in.ExpiryExhaustTarget = "1M/1M"
		}
		if action == "switch_plan" {
			in.ExpiryExhaustTarget = "11111111-1111-1111-1111-111111111111"
		}
		fe := in.validate()
		if hasFieldError(fe, "expiry_behavior") {
			t.Errorf("action %q: unexpected expiry_behavior error: %+v", action, fe)
		}
	}
}

func TestValidate_ExpiryThrottleRequiresTarget(t *testing.T) {
	in := baseValidInput()
	in.ExpiryBehavior = "throttle"
	fe := in.validate()
	if !hasFieldError(fe, "expiry_exhaust_target") {
		t.Fatalf("expected expiry_exhaust_target error, got %+v", fe)
	}
}

func TestValidate_ExpirySwitchPlanRequiresTarget(t *testing.T) {
	in := baseValidInput()
	in.ExpiryBehavior = "switch_plan"
	fe := in.validate()
	if !hasFieldError(fe, "expiry_exhaust_target") {
		t.Fatalf("expected expiry_exhaust_target error, got %+v", fe)
	}
}

func TestValidate_ExpiryRejectsUnknownAction(t *testing.T) {
	in := baseValidInput()
	in.ExpiryBehavior = "delete_everything"
	fe := in.validate()
	if !hasFieldError(fe, "expiry_behavior") {
		t.Fatalf("expected expiry_behavior error for garbage action, got %+v", fe)
	}
}

func TestValidate_QuotaSwitchPlanRequiresTarget(t *testing.T) {
	in := baseValidInput()
	in.QuotaBehavior = "switch_plan"
	fe := in.validate()
	if !hasFieldError(fe, "quota_exhaust_target") {
		t.Fatalf("expected quota_exhaust_target error, got %+v", fe)
	}
}

func TestValidate_QuotaThrottleStillRequiresThrottleRate(t *testing.T) {
	// Regression: FR-130's widening must not disturb the pre-existing rule.
	in := baseValidInput()
	in.QuotaBehavior = "throttle"
	fe := in.validate()
	if !hasFieldError(fe, "throttle_rate") {
		t.Fatalf("expected throttle_rate error (pre-existing rule), got %+v", fe)
	}
}

func TestValidate_UptimeUnknownActionRejected(t *testing.T) {
	in := baseValidInput()
	in.UptimeExhaustAction = "nonsense"
	fe := in.validate()
	if !hasFieldError(fe, "uptime_exhaust_action") {
		t.Fatalf("expected uptime_exhaust_action error, got %+v", fe)
	}
}

func TestValidate_UptimeThrottleRequiresTargetOnlyWhenLimited(t *testing.T) {
	in := baseValidInput()
	in.UptimeExhaustAction = "throttle"
	// No UptimeLimitSeconds set (unlimited): an unused action's missing
	// target must not block saving the plan.
	if fe := in.validate(); hasFieldError(fe, "uptime_exhaust_target") {
		t.Fatalf("did not expect uptime_exhaust_target error when unlimited, got %+v", fe)
	}

	limit := int64(3600)
	in2 := baseValidInput()
	in2.UptimeExhaustAction = "throttle"
	in2.UptimeLimitSeconds = &limit
	fe := in2.validate()
	if !hasFieldError(fe, "uptime_exhaust_target") {
		t.Fatalf("expected uptime_exhaust_target error when limited, got %+v", fe)
	}
}

func TestValidate_UptimeNegativeLimitRejected(t *testing.T) {
	limit := int64(-1)
	in := baseValidInput()
	in.UptimeLimitSeconds = &limit
	fe := in.validate()
	if !hasFieldError(fe, "uptime_limit_seconds") {
		t.Fatalf("expected uptime_limit_seconds error, got %+v", fe)
	}
}

func TestValidate_DailyResetMinRange(t *testing.T) {
	in := baseValidInput()
	in.DailyResetMin = 1440
	fe := in.validate()
	if !hasFieldError(fe, "daily_reset_min") {
		t.Fatalf("expected daily_reset_min error, got %+v", fe)
	}
}

func TestValidate_DailyBlockAllowedExplicitly(t *testing.T) {
	limit := int64(1_000_000_000)
	in := baseValidInput()
	in.DailyLimitBytes = &limit
	in.DailyExhaustAction = "block"
	fe := in.validate()
	if len(fe) != 0 {
		t.Fatalf("expected daily block to validate cleanly, got %+v", fe)
	}
}
