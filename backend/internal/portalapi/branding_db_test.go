package portalapi_test

// DB-backed regression test for the public GET /api/v1/branding endpoint
// (v2 phase 11, FR-91): it used to read a settings key, "branding", that has
// never existed (platform.Settings stores one row per fully-qualified key
// like "branding.name"), so it always silently served the hardcoded default
// regardless of what Settings > Branding had saved. See
// docs/ops/known-issues.md.

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/hikrad/hikrad/internal/platform"
)

type brandingView struct {
	Name            string  `json:"name"`
	LogoURL         *string `json:"logo_url"`
	ThemeColor      *string `json:"theme_color"`
	BackgroundColor *string `json:"background_color"`
}

func TestBrandingEndpointReflectsConfiguredIdentity(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	settings := platform.NewSettings(e.db)

	if err := settings.Set(ctx, "branding.name", "Nur Net"); err != nil {
		t.Fatalf("set branding.name: %v", err)
	}
	if err := settings.Set(ctx, "branding.primary_color", "#08748f"); err != nil {
		t.Fatalf("set branding.primary_color: %v", err)
	}
	if err := settings.Set(ctx, "branding.secondary_color", "#0f172a"); err != nil {
		t.Fatalf("set branding.secondary_color: %v", err)
	}

	// Unauthenticated GET (contract C3: public, read-only).
	r := e.do(t, "GET", "/api/v1/branding", "", nil)
	if r.status != http.StatusOK {
		t.Fatalf("GET /api/v1/branding = %d: %s", r.status, r.body)
	}
	var b brandingView
	if err := json.Unmarshal(r.body, &b); err != nil {
		t.Fatalf("unmarshal: %v (%s)", err, r.body)
	}
	if b.Name != "Nur Net" {
		t.Errorf("branding.name = %q, want %q (the pre-phase bug always returned the hardcoded default here)", b.Name, "Nur Net")
	}
	if b.ThemeColor == nil || *b.ThemeColor != "#08748f" {
		t.Errorf("theme_color = %v, want #08748f", b.ThemeColor)
	}
	if b.BackgroundColor == nil || *b.BackgroundColor != "#0f172a" {
		t.Errorf("background_color = %v, want #0f172a (this field was never populated at all pre-phase)", b.BackgroundColor)
	}
}
