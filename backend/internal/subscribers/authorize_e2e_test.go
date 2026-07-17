package subscribers_test

// End-to-end proof that D's AuthView read-model drives B's authorize policy
// engine (gate item 1). These hit the real /internal/radius/authorize with a
// NAS registered through the API (so B's known-NAS cache invalidates at once)
// and subscribers/profiles created through D's API, exercising the FR-9
// expired-pool, FR-1 disabled, and FR-58 Hotspot-gating branches with real data.

import (
	"context"
	"fmt"
	"math/rand"
	"net/http"
	"testing"
	"time"
)

type authzAttr struct {
	Intent string `json:"intent"`
	Value  string `json:"value"`
}

type authzResult struct {
	Action     string      `json:"action"`
	Reason     string      `json:"reason"`
	Attributes []authzAttr `json:"attributes"`
}

// registerNAS creates a NAS via the API (invalidating B's known-NAS cache) and
// returns its source IP for authorize requests.
func (e testEnv) registerNAS(t *testing.T) string {
	t.Helper()
	ip := fmt.Sprintf("10.%d.%d.1", rand.Intn(200)+10, rand.Intn(200)+10)
	// Both services, so the FR-58 hotspot legs below resolve to an instance
	// (v2 phase 1: a hotspot login on a NAS with no hotspot service rejects
	// nas_not_allowed before ever reaching the subscriber's service_type).
	r := e.do(t, "POST", "/api/v1/nas", map[string]any{
		"name": uniq("nas_"), "ip": ip, "secret": "testing123",
		"services": []map[string]any{
			{"service": "pppoe", "label": "e2e-pppoe"},
			{"service": "hotspot", "label": "e2e-hotspot"},
		},
	})
	if r.status != http.StatusCreated {
		t.Fatalf("register NAS = %d: %s", r.status, r.body)
	}
	return ip
}

func (e testEnv) authorize(t *testing.T, user, pass, nasIP, service string) authzResult {
	t.Helper()
	// Unauthenticated internal route; post directly.
	r := e.do(t, "POST", "/internal/radius/authorize", map[string]any{
		"username": user, "password": pass, "nas_ip": nasIP, "service": service,
	})
	if r.status != http.StatusOK {
		t.Fatalf("authorize %s/%s = %d: %s", user, service, r.status, r.body)
	}
	var out authzResult
	r.into(t, &out)
	return out
}

func TestAuthViewDrivesAuthorize(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	nasIP := e.registerNAS(t)

	// A profile whose expiry behavior is the walled-garden expired pool (FR-9B),
	// with a Hotspot rate so FR-58 can be checked.
	r := e.do(t, "POST", "/api/v1/profiles", map[string]any{
		"name": uniq("Plan_"), "price": 20000, "duration_days": 30,
		"rate_down_kbps": 10240, "rate_up_kbps": 10240,
		"expiry_behavior": "expired_pool", "hotspot_rate_down_kbps": 5120, "hotspot_rate_up_kbps": 5120,
	})
	if r.status != http.StatusCreated {
		t.Fatalf("create profile = %d: %s", r.status, r.body)
	}
	var prof struct {
		ID string `json:"id"`
	}
	r.into(t, &prof)

	// A system expired-purpose pool the AuthView resolves ExpiredPoolName from.
	poolName := uniq("garden_")
	if _, err := e.db.Exec(ctx,
		`INSERT INTO ip_pools (name, ranges, purpose) VALUES ($1, ARRAY['10.99.0.0/24']::inet[], 'expired')`,
		poolName); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = e.db.Exec(context.Background(), `DELETE FROM ip_pools WHERE name = $1`, poolName) })

	// --- Active subscriber, allow_hotspot on ---------------------------------
	active := uniq("act_")
	mkSub(t, e, active, "pw1", prof.ID, time.Now().Add(30*24*time.Hour), true, "active")

	got := e.authorize(t, active, "pw1", nasIP, "pppoe")
	if got.Action != "accept" || rateOf(got) != "10M/10M" {
		t.Errorf("active pppoe: %+v (want accept 10M/10M)", got)
	}
	// FR-58: Hotspot login for the flagged subscriber accepts with the Hotspot rate.
	got = e.authorize(t, active, "pw1", nasIP, "hotspot")
	if got.Action != "accept" || rateOf(got) != "5M/5M" {
		t.Errorf("active hotspot: %+v (want accept 5M/5M)", got)
	}

	// --- allow_hotspot off → hotspot rejected with service_not_allowed -------
	noHS := uniq("nohs_")
	mkSub(t, e, noHS, "pw2", prof.ID, time.Now().Add(30*24*time.Hour), false, "active")
	got = e.authorize(t, noHS, "pw2", nasIP, "hotspot")
	if got.Action != "reject" || got.Reason != "service_not_allowed" {
		t.Errorf("hotspot flag off: %+v (want reject service_not_allowed)", got)
	}

	// --- Expired subscriber, expired_pool behavior → walled-garden accept ----
	expired := uniq("exp_")
	mkSub(t, e, expired, "pw3", prof.ID, time.Now().Add(-24*time.Hour), false, "active")
	got = e.authorize(t, expired, "pw3", nasIP, "pppoe")
	if got.Action != "accept" {
		t.Fatalf("expired expired_pool: %+v (want accept)", got)
	}
	if intentOf(got, "address_pool") != poolName {
		t.Errorf("expired accept missing expired pool %q: %+v", poolName, got)
	}

	// --- Disabled subscriber → reject disabled -------------------------------
	disabled := uniq("dis_")
	mkSub(t, e, disabled, "pw4", prof.ID, time.Now().Add(30*24*time.Hour), false, "disabled")
	got = e.authorize(t, disabled, "pw4", nasIP, "pppoe")
	if got.Action != "reject" || got.Reason != "disabled" {
		t.Errorf("disabled: %+v (want reject disabled)", got)
	}

	// --- Wrong password → reject bad_password --------------------------------
	got = e.authorize(t, active, "nope", nasIP, "pppoe")
	if got.Action != "reject" || got.Reason != "bad_password" {
		t.Errorf("bad password: %+v (want reject bad_password)", got)
	}
}

// mkSub creates a subscriber. hotspot maps to the FR-61 service_type exactly as
// migration 0500 maps v1's allow_hotspot bit (true -> dual, false -> pppoe), so
// these assertions keep testing precisely what they tested before (C1).
func mkSub(t *testing.T, e testEnv, user, pass, profileID string, expires time.Time, hotspot bool, status string) {
	t.Helper()
	serviceType := "pppoe"
	if hotspot {
		serviceType = "dual"
	}
	r := e.do(t, "POST", "/api/v1/subscribers", map[string]any{
		"username": user, "password": pass, "profile_id": profileID,
		"expires_at": expires.UTC().Format(time.RFC3339), "service_type": serviceType, "status": status,
	})
	if r.status != http.StatusCreated {
		t.Fatalf("create subscriber %s = %d: %s", user, r.status, r.body)
	}
}

func rateOf(r authzResult) string { return intentOf(r, "rate_limit") }

func intentOf(r authzResult, intent string) string {
	for _, a := range r.Attributes {
		if a.Intent == intent {
			return a.Value
		}
	}
	return ""
}
