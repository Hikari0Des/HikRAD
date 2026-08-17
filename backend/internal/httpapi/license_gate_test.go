package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hikrad/hikrad/internal/platform"
	"github.com/hikrad/hikrad/internal/platform/license"
)

// TestLicenseGateCoverageMap is the "read-only middleware coverage map" from
// Agent 1's Phase-5 definition of done: every representative route/method
// combination the gate must (or must not) block once the license is
// expired_grace.
func TestLicenseGateCoverageMap(t *testing.T) {
	cases := []struct {
		name    string
		method  string
		path    string
		blocked bool
	}{
		{"read never blocked: GET subscribers", http.MethodGet, "/api/v1/subscribers", false},
		{"read never blocked: GET live sessions", http.MethodGet, "/api/v1/live/sessions", false},
		{"mutation blocked: POST subscribers", http.MethodPost, "/api/v1/subscribers", true},
		{"mutation blocked: PUT profile", http.MethodPut, "/api/v1/profiles/123", true},
		{"mutation blocked: DELETE nas", http.MethodDelete, "/api/v1/nas/1", true},
		{"mutation blocked: PATCH settings", http.MethodPatch, "/api/v1/settings/billing", true},
		{"RADIUS internal never blocked: POST authorize", http.MethodPost, "/internal/radius/authorize", false},
		{"RADIUS internal never blocked: POST coa", http.MethodPost, "/internal/radius/coa", false},
		{"license upload exempt: POST license", http.MethodPost, "/api/v1/license", false},
		{"license request-blob exempt: POST", http.MethodPost, "/api/v1/license/request-blob", false},
		{"wizard exempt: POST setup admin", http.MethodPost, "/api/v1/setup/admin", false},
		{"auth exempt: POST login", http.MethodPost, "/api/v1/auth/login", false},
		{"auth exempt: POST refresh", http.MethodPost, "/api/v1/auth/refresh", false},
	}

	setLicenseStateForTest(t, license.StateExpiredGrace)

	handler := licenseGate(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := httptest.NewRequest(c.method, c.path, nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			blocked := rec.Code == http.StatusForbidden
			if blocked != c.blocked {
				t.Errorf("%s %s: blocked=%v, want %v (status %d)", c.method, c.path, blocked, c.blocked, rec.Code)
			}
		})
	}
}

func TestLicenseGateAllowsMutationsWhenNotExpired(t *testing.T) {
	for _, st := range []license.State{license.StateValid, license.StateGrace, ""} {
		setLicenseStateForTest(t, st)
		handler := licenseGate(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		req := httptest.NewRequest(http.MethodPost, "/api/v1/subscribers", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("state %q: mutation blocked (status %d), want allowed", st, rec.Code)
		}
	}
}

// TestLicenseGateNeverBlocksInternalPrefix_ViaExpiredSnapshot is phase
// v4-3's restated invariant (FR-124: "no new blocking point may be
// introduced by anything in this phase; a test asserts it"), driven through
// the full LicenseSnapshot the way a genuinely expired v2 license (calendar
// expiry, not just fingerprint drift) actually produces it — licenseGate
// keys only on the resulting State, never on why it's expired_grace, so a
// calendar-expired license exercises exactly the same exemption list
// TestLicenseGateCoverageMap already proves for a fingerprint-drift one.
// internal/radius never imports internal/platform's license cache at all
// (confirmed by inspection — there is no CachedLicenseState/
// LicenseHasEntitlement call anywhere under internal/radius), and
// hikrad-acct (accounting ingest) never wires httpapi.NewRouter or
// licenseGate at all — it is a separate binary with its own listener, so
// this middleware cannot run in that process regardless of state. Both
// are what make "no new blocking point" true by construction, not just by
// this one exemption-list check; this test is the behavioral half of the
// claim for the one process (hikrad-api) where the gate does run.
func TestLicenseGateNeverBlocksInternalPrefix_ViaExpiredSnapshot(t *testing.T) {
	past := time.Now().Add(-24 * time.Hour)
	platform.SetCachedLicenseSnapshotForTest(platform.LicenseSnapshot{
		State:        license.StateExpiredGrace,
		Entitlements: map[string]bool{},
		ExpiresAt:    &past,
	})
	t.Cleanup(func() { platform.SetCachedLicenseSnapshotForTest(platform.LicenseSnapshot{State: license.StateValid}) })

	handler := licenseGate(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// The only two real /internal/radius/* routes on this router (CoA is sent
	// out by hikrad-api directly, never received over HTTP; accounting
	// ingest runs in the separate hikrad-acct binary, which doesn't run this
	// middleware at all — see the comment above).
	internalPaths := []struct {
		method, path string
	}{
		{http.MethodPost, "/internal/radius/authorize"},
		{http.MethodPost, "/internal/radius/dynamic-client"},
	}
	for _, c := range internalPaths {
		t.Run(c.path, func(t *testing.T) {
			req := httptest.NewRequest(c.method, c.path, nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code == http.StatusForbidden {
				t.Fatalf("%s %s was blocked while expired_grace (from calendar expiry) — RADIUS/accounting must never gate", c.method, c.path)
			}
		})
	}

	// A panel write must still be gated normally — this test isn't
	// accidentally passing because the gate stopped working entirely.
	req := httptest.NewRequest(http.MethodPost, "/api/v1/subscribers", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("panel write during expired_grace = %d, want 403 (gate must still block panel writes)", rec.Code)
	}
}

// setLicenseStateForTest drives platform's process-wide cache through its
// only mutator (RefreshLicenseCache reads the DB; there is no test seam), so
// instead we exploit that platform.CachedLicenseState defaults to "valid" and
// there is no license row: we can't set arbitrary states without a DB. To
// keep this test DB-free, we instead call the unexported test hook the
// platform package exposes for exactly this purpose.
func setLicenseStateForTest(t *testing.T, s license.State) {
	t.Helper()
	platform.SetCachedLicenseStateForTest(s)
	t.Cleanup(func() { platform.SetCachedLicenseStateForTest("") })
}
