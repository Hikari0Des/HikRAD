package portalapi

// GET /api/v1/branding (contract C5, public/unauthenticated): ISP
// name/colors/logo for manifests and the login page — no rebuild per ISP
// (FR-43/FR-91). GET /api/v1/branding/logo (v2 phase 11, FR-91, contract C3)
// serves the raw logo bytes. Both read through platform.LoadIdentity /
// platform.ReadLogoBytes — the single corrected branding source (see
// docs/ops/known-issues.md for the pre-phase bug this replaces: this handler
// used to read a settings key, "branding", that has never existed).

import (
	"net/http"

	"github.com/hikrad/hikrad/internal/httpapi"
	"github.com/hikrad/hikrad/internal/platform"
	"github.com/hikrad/hikrad/internal/platform/license"
)

type brandingResponse struct {
	Name            string  `json:"name"`
	LogoURL         *string `json:"logo_url"`
	ThemeColor      *string `json:"theme_color"`
	BackgroundColor *string `json:"background_color"`

	// WhiteLabel (FR-122, Decision 62) is true only when the installed
	// license's SIGNED payload grants the white_label entitlement — never a
	// settings value. This is the one field on this public, unauthenticated
	// endpoint that both PoweredByFooter components (panel and portal) read
	// to decide whether to render the HikRAD attribution mark; there is no
	// settings key, no PUT, and no panel UI path anywhere that writes this
	// value — it is derived fresh from platform.LicenseHasEntitlement on
	// every request.
	WhiteLabel bool `json:"white_label"`
}

func (m *Module) brandingHandler(w http.ResponseWriter, r *http.Request) {
	resp := brandingResponse{Name: "HikRAD"}
	if m.settings != nil {
		id := platform.LoadIdentity(r.Context(), m.settings)
		if id.Name != "" {
			resp.Name = id.Name
		}
		resp.LogoURL = id.LogoURL
		resp.ThemeColor = id.ThemeColor
		resp.BackgroundColor = id.BackgroundColor
	}
	resp.WhiteLabel = platform.LicenseHasEntitlement(license.EntitlementWhiteLabel)
	httpapi.JSON(w, http.StatusOK, resp)
}

// brandingLogoHandler serves the raw logo bytes with a long-lived,
// content-addressed Cache-Control (safe: the URL's ?v= query changes
// whenever the content does, so a stale cached copy is never served under a
// URL that still resolves).
func (m *Module) brandingLogoHandler(w http.ResponseWriter, r *http.Request) {
	data, contentType, ok := platform.ReadLogoBytes()
	if !ok {
		httpapi.Error(w, http.StatusNotFound, "not_found", "no logo is currently set")
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}
