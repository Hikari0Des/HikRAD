package subscribers_test

// FR-149: a per-subscriber static IPv6 prefix pin is settable through the API
// (validated, stored canonical, clearable) and reaches the RADIUS reply as the
// delegated_prefix intent. Before this, the column existed (migration 0611)
// but nothing could write it and the AuthView never read it.

import (
	"net/http"
	"testing"
	"time"
)

func TestIPv6StaticPrefix_SetValidateAndReachesAuthorize(t *testing.T) {
	e := setup(t)
	nasIP := e.registerNAS(t)
	profileID := e.createProfile(t, uniq("v6plan_"), 10240, 10240)

	// Rejected shapes never persist.
	for _, bad := range []string{"10.0.0.0/24", "2001:db8::/16", "not-a-prefix", "2001:db8::/80"} {
		r := e.do(t, "POST", "/api/v1/subscribers", map[string]any{
			"username": uniq("v6bad_"), "password": "pw", "profile_id": profileID,
			"ipv6_static_prefix": bad,
		})
		if r.status != http.StatusUnprocessableEntity {
			t.Errorf("ipv6_static_prefix %q = %d, want 422: %s", bad, r.status, r.body)
		}
	}

	// Accepted, and normalized to the canonical network form.
	uname := uniq("v6_")
	r := e.do(t, "POST", "/api/v1/subscribers", map[string]any{
		"username": uname, "password": "pw", "profile_id": profileID,
		"expires_at":         time.Now().Add(30 * 24 * time.Hour).Format(time.RFC3339),
		"ipv6_static_prefix": " 2001:DB8:10:0::1/56 ",
	})
	if r.status != http.StatusCreated {
		t.Fatalf("create = %d: %s", r.status, r.body)
	}
	var sub struct {
		ID               string  `json:"id"`
		IPv6StaticPrefix *string `json:"ipv6_static_prefix"`
	}
	r.into(t, &sub)
	if sub.IPv6StaticPrefix == nil || *sub.IPv6StaticPrefix != "2001:db8:10::/56" {
		t.Fatalf("stored prefix = %v, want 2001:db8:10::/56", sub.IPv6StaticPrefix)
	}

	got := e.authorize(t, uname, "pw", nasIP, "pppoe")
	if got.Action != "accept" {
		t.Fatalf("authorize: %+v (want accept)", got)
	}
	if p := intentOf(got, "delegated_prefix"); p != "2001:db8:10::/56" {
		t.Errorf("delegated_prefix intent = %q, want 2001:db8:10::/56 (%+v)", p, got)
	}

	// An explicit "" clears the pin; the reply then carries no prefix.
	u := e.do(t, "PUT", "/api/v1/subscribers/"+sub.ID, map[string]any{"ipv6_static_prefix": ""})
	if u.status != http.StatusOK {
		t.Fatalf("clear = %d: %s", u.status, u.body)
	}
	var cleared struct {
		IPv6StaticPrefix *string `json:"ipv6_static_prefix"`
	}
	u.into(t, &cleared)
	if cleared.IPv6StaticPrefix != nil {
		t.Errorf("after clear prefix = %q, want null", *cleared.IPv6StaticPrefix)
	}
	got = e.authorize(t, uname, "pw", nasIP, "pppoe")
	if p := intentOf(got, "delegated_prefix"); p != "" {
		t.Errorf("delegated_prefix after clear = %q, want none", p)
	}
}
