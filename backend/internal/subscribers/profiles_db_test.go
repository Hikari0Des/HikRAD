package subscribers_test

import (
	"net/http"
	"testing"
)

func TestProfileValidation(t *testing.T) {
	e := setup(t)

	// quota_mode=total requires quota_total_bytes.
	if r := e.do(t, "POST", "/api/v1/profiles", map[string]any{
		"name": uniq("Q_"), "price_iqd": 1, "duration_days": 30,
		"rate_down_kbps": 1024, "rate_up_kbps": 1024, "quota_mode": "total",
	}); r.status != http.StatusUnprocessableEntity {
		t.Errorf("quota total without bytes = %d, want 422: %s", r.status, r.body)
	}
	// quota_behavior=throttle requires throttle_rate.
	if r := e.do(t, "POST", "/api/v1/profiles", map[string]any{
		"name": uniq("T_"), "price_iqd": 1, "duration_days": 30,
		"rate_down_kbps": 1024, "rate_up_kbps": 1024, "quota_behavior": "throttle",
	}); r.status != http.StatusUnprocessableEntity {
		t.Errorf("throttle without rate = %d, want 422: %s", r.status, r.body)
	}
	// Valid profile with a total quota → created, defaults applied.
	r := e.do(t, "POST", "/api/v1/profiles", map[string]any{
		"name": uniq("OK_"), "price_iqd": 1000, "duration_days": 30,
		"rate_down_kbps": 10240, "rate_up_kbps": 10240,
		"quota_mode": "total", "quota_total_bytes": 100000000000,
	})
	if r.status != http.StatusCreated {
		t.Fatalf("valid profile = %d: %s", r.status, r.body)
	}
	var p struct {
		ID             string `json:"id"`
		ExpiryBehavior string `json:"expiry_behavior"`
		QuotaBehavior  string `json:"quota_behavior"`
	}
	r.into(t, &p)
	if p.ExpiryBehavior != "block" || p.QuotaBehavior != "block" {
		t.Errorf("defaults not applied: %+v", p)
	}
}

func TestProfileApplyNowSemantics(t *testing.T) {
	e := setup(t)
	profID := e.createProfile(t, uniq("App_"), 10240, 10240)
	// A subscriber on the profile (offline — no live session).
	if r := e.do(t, "POST", "/api/v1/subscribers", map[string]any{
		"username": uniq("app_"), "password": "x", "profile_id": profID,
	}); r.status != http.StatusCreated {
		t.Fatalf("seed subscriber = %d: %s", r.status, r.body)
	}

	// apply=now (default): reports applied=now + online_affected (empty offline).
	r := e.do(t, "PUT", "/api/v1/profiles/"+profID, map[string]any{
		"name": uniq("App2_"), "price_iqd": 2000, "duration_days": 30,
		"rate_down_kbps": 20480, "rate_up_kbps": 20480,
	})
	if r.status != http.StatusOK {
		t.Fatalf("update apply-now = %d: %s", r.status, r.body)
	}
	var out struct {
		Applied        string           `json:"applied"`
		OnlineAffected []map[string]any `json:"online_affected"`
	}
	r.into(t, &out)
	if out.Applied != "now" || out.OnlineAffected == nil {
		t.Errorf("apply-now response = %+v", out)
	}
	if e.auditCount(t, "profile.update", profID) < 1 {
		t.Errorf("profile.update not audited")
	}

	// apply=next_renewal: persists only.
	r = e.do(t, "PUT", "/api/v1/profiles/"+profID+"?apply=next_renewal", map[string]any{
		"name": uniq("App3_"), "price_iqd": 3000, "duration_days": 30,
		"rate_down_kbps": 30720, "rate_up_kbps": 30720,
	})
	r.into(t, &out)
	if out.Applied != "next_renewal" {
		t.Errorf("apply=next_renewal response = %+v", out)
	}
}
