package httpapi

// LicenseGate enforces FR-50.3 / contract C4's "after grace expiry: panel
// becomes read-only but RADIUS auth/accounting keep running" — it never
// blocks a GET (read) request and never blocks /internal/* (FreeRADIUS's
// policy/CoA endpoints), only mutating panel/portal API calls once the
// license is expired_grace. It lives in the frozen global middleware chain
// (NewRouter, alongside enforceJSON) because "every mutating endpoint" is
// exactly the set no single domain module can enumerate — the same reason
// enforceJSON is global rather than per-module.
//
// It reads internal/platform's process-wide license cache (Agent 1,
// Phase 5); platform cannot depend on httpapi (httpapi already depends on
// platform for the Deps.Settings type), so the gate is implemented here
// rather than in internal/platform.

import (
	"net/http"
	"strings"

	"github.com/hikrad/hikrad/internal/platform"
	"github.com/hikrad/hikrad/internal/platform/license"
)

// licenseGateExemptPrefixes are mutating routes that must keep working even
// in expired_grace: license re-upload/request-blob (the only way out), the
// first-run wizard (runs before any license exists), and session endpoints
// (login/refresh/logout — an admin who can't log in can't see the read-only
// banner or fix the license).
var licenseGateExemptPrefixes = []string{
	"/internal/",
	"/api/v1/setup/",
	"/api/v1/license",
	"/api/v1/auth/",
}

func isMutatingMethod(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

func licenseGateExempt(path string) bool {
	for _, p := range licenseGateExemptPrefixes {
		if strings.HasPrefix(path, p) {
			return true
		}
	}
	return false
}

// licenseGate is the license.expired_grace middleware (contract C4).
func licenseGate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isMutatingMethod(r.Method) && !licenseGateExempt(r.URL.Path) &&
			platform.CachedLicenseState() == license.StateExpiredGrace {
			Error(w, http.StatusForbidden, "license_expired",
				"the license grace period has expired; panel changes are disabled until a valid license is installed")
			return
		}
		next.ServeHTTP(w, r)
	})
}
