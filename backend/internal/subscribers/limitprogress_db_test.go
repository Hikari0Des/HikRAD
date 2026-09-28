package subscribers_test

// FR-135 list-depth DB coverage: the batched per-row limit progress the
// subscriber list attaches (quota/uptime/daily bars, days remaining, and the
// plan-swap badge for a currently-active switch_plan exhaustion).

import (
	"net/http"
	"testing"
	"time"
)

func TestList_LimitProgress_QuotaExhaustedTriggersPlanSwapBadge(t *testing.T) {
	e := setup(t)

	// Target plan for the swap.
	targetID := e.createProfile(t, "target-plan", 2048, 2048)

	// Source plan: tiny quota, quota_behavior=switch_plan -> targetID.
	pr := e.do(t, "POST", "/api/v1/profiles", map[string]any{
		"name": "swap-source", "price": 5000, "duration_days": 30,
		"rate_down_kbps": 1024, "rate_up_kbps": 1024,
		"quota_mode": "total", "quota_total_bytes": 100,
		"quota_behavior": "switch_plan", "quota_exhaust_target": targetID,
		"expiry_behavior": "block",
	})
	if pr.status != http.StatusCreated {
		t.Fatalf("create source profile = %d: %s", pr.status, pr.body)
	}
	var prof struct {
		ID string `json:"id"`
	}
	pr.into(t, &prof)

	uname := e.prefix + "swap"
	sr := e.do(t, "POST", "/api/v1/subscribers", map[string]any{
		"username": uname, "password": "x", "profile_id": prof.ID,
		"expires_at": time.Now().Add(30 * 24 * time.Hour).Format(time.RFC3339),
	})
	if sr.status != http.StatusCreated {
		t.Fatalf("create subscriber = %d: %s", sr.status, sr.body)
	}
	var sub struct {
		ID string `json:"id"`
	}
	sr.into(t, &sub)

	// Push usage past the 100-byte quota directly into usage_points (the
	// accounting pipeline's own write path — this test only needs the data
	// the list query reads, not to re-drive accounting itself).
	if _, err := e.db.Exec(t.Context(),
		`INSERT INTO usage_points (time, subscriber_id, nas_id, delta_down, delta_up, service)
		 VALUES (now(), $1::uuid, gen_random_uuid(), 60, 60, 'pppoe')`, sub.ID); err != nil {
		t.Fatalf("seed usage: %v", err)
	}

	lr := e.do(t, "GET", "/api/v1/subscribers?q="+uname, nil)
	if lr.status != http.StatusOK {
		t.Fatalf("list = %d: %s", lr.status, lr.body)
	}
	var out struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
		Limits map[string]struct {
			QuotaUsedBytes  int64 `json:"quota_used_bytes"`
			QuotaTotalBytes int64 `json:"quota_total_bytes"`
			DaysRemaining   *int  `json:"days_remaining"`
			PlanSwap        struct {
				Active        bool   `json:"active"`
				ToProfileID   string `json:"to_profile_id"`
				ToProfileName string `json:"to_profile_name"`
			} `json:"plan_swap"`
		} `json:"limits"`
	}
	lr.into(t, &out)

	if len(out.Items) != 1 {
		t.Fatalf("expected exactly 1 matching row, got %d", len(out.Items))
	}
	lp, ok := out.Limits[sub.ID]
	if !ok {
		t.Fatalf("no limit progress attached for subscriber %s: %+v", sub.ID, out.Limits)
	}
	if lp.QuotaUsedBytes != 120 {
		t.Errorf("quota_used_bytes = %d, want 120", lp.QuotaUsedBytes)
	}
	if lp.QuotaTotalBytes != 100 {
		t.Errorf("quota_total_bytes = %d, want 100", lp.QuotaTotalBytes)
	}
	if lp.DaysRemaining == nil || *lp.DaysRemaining < 28 || *lp.DaysRemaining > 30 {
		t.Errorf("days_remaining = %v, want ~30", lp.DaysRemaining)
	}
	if !lp.PlanSwap.Active {
		t.Fatalf("plan_swap.active = false, want true (quota exhausted with switch_plan behavior)")
	}
	if lp.PlanSwap.ToProfileID != targetID {
		t.Errorf("plan_swap.to_profile_id = %q, want %q", lp.PlanSwap.ToProfileID, targetID)
	}
	if lp.PlanSwap.ToProfileName != "target-plan" {
		t.Errorf("plan_swap.to_profile_name = %q, want target-plan", lp.PlanSwap.ToProfileName)
	}
}

// An online subscriber (open session, sub-second timestamps — every real
// session) made the uptime SUM(EXTRACT(EPOCH …)) fractional; scanning it into
// int64 failed and the WHOLE subscriber list page returned 500 whenever such a
// subscriber was on it. Uptime is now reported in whole seconds.
func TestList_LimitProgress_OpenSessionWithFractionalSecondsDoesNot500(t *testing.T) {
	e := setup(t)
	profileID := e.createProfile(t, "uptime-frac", 1024, 1024)

	uname := e.prefix + "online"
	sr := e.do(t, "POST", "/api/v1/subscribers", map[string]any{
		"username": uname, "password": "x", "profile_id": profileID,
		"expires_at": time.Now().Add(30 * 24 * time.Hour).Format(time.RFC3339),
	})
	if sr.status != http.StatusCreated {
		t.Fatalf("create subscriber = %d: %s", sr.status, sr.body)
	}
	var sub struct {
		ID string `json:"id"`
	}
	sr.into(t, &sub)

	// Uptime counts from the cycle anchor, which defaults to created_at — so
	// backdate the account or a session older than it is clipped to ~0s.
	if _, err := e.db.Exec(t.Context(),
		`UPDATE subscribers SET created_at = now() - interval '1 hour' WHERE id = $1::uuid`, sub.ID); err != nil {
		t.Fatalf("backdate subscriber: %v", err)
	}
	// Still-open session that started 90.5s ago: COALESCE(stopped_at, now())
	// minus a microsecond-precision started_at is never a whole number.
	if _, err := e.db.Exec(t.Context(),
		`INSERT INTO sessions (nas_id, acct_session_id, subscriber_id, username, started_at)
		 VALUES (gen_random_uuid(), $1, $2::uuid, $3, now() - interval '90.5 seconds')`,
		e.prefix+"frac-sess", sub.ID, uname); err != nil {
		t.Fatalf("seed session: %v", err)
	}

	lr := e.do(t, "GET", "/api/v1/subscribers?q="+uname, nil)
	if lr.status != http.StatusOK {
		t.Fatalf("list = %d, want 200: %s", lr.status, lr.body)
	}
	var out struct {
		Limits map[string]struct {
			UptimeUsedSeconds int64 `json:"uptime_used_seconds"`
		} `json:"limits"`
	}
	lr.into(t, &out)
	lp, ok := out.Limits[sub.ID]
	if !ok {
		t.Fatalf("no limit progress attached for subscriber %s: %+v", sub.ID, out.Limits)
	}
	if lp.UptimeUsedSeconds < 90 || lp.UptimeUsedSeconds > 120 {
		t.Errorf("uptime_used_seconds = %d, want ~90", lp.UptimeUsedSeconds)
	}
}
