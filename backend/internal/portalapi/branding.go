package portalapi

// GET /api/v1/branding (contract C5, public/unauthenticated): ISP
// name/colors/logo for manifests and the login page — no rebuild per ISP
// (FR-43). Reads the same "branding" settings group internal/radius/hotspot.go
// already uses for the Hotspot login page, so admins configure branding once
// (Phase 5's settings screen) and every surface (Hotspot, portal, panel PWA
// manifests) renders it consistently. Response field names match F's
// already-written client (frontend/portal/src/api/branding.ts) exactly.

import (
	"net/http"

	"github.com/hikrad/hikrad/internal/httpapi"
	"github.com/hikrad/hikrad/internal/platform"
)

type brandingSettings struct {
	Name         string `json:"name"`
	ColorPrimary string `json:"color_primary"`
	LogoDataURI  string `json:"logo_data_uri"`
}

type brandingResponse struct {
	Name            string  `json:"name"`
	LogoURL         *string `json:"logo_url"`
	ThemeColor      *string `json:"theme_color"`
	BackgroundColor *string `json:"background_color"`
}

func (m *Module) brandingHandler(w http.ResponseWriter, r *http.Request) {
	resp := brandingResponse{Name: "HikRAD Wi-Fi"}
	if m.settings != nil {
		if b, err := platform.Get[brandingSettings](r.Context(), m.settings, "branding"); err == nil {
			if b.Name != "" {
				resp.Name = b.Name
			}
			if b.ColorPrimary != "" {
				resp.ThemeColor = &b.ColorPrimary
			}
			if b.LogoDataURI != "" {
				resp.LogoURL = &b.LogoDataURI
			}
		}
	}
	httpapi.JSON(w, http.StatusOK, resp)
}
