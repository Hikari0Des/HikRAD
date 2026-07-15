package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"

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
