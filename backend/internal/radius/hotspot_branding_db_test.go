package radius

// DB-backed regression test for the Hotspot captive-portal package's
// branding bug (v2 phase 11, FR-91): loadHotspotBranding used to read a
// settings key, "branding", that has never existed (see
// docs/ops/known-issues.md), so the generated login page always rendered
// the hardcoded default regardless of what Settings > Branding had saved.
// This proves the fix reads the real configured identity end to end,
// through the same platform.LoadIdentity/ReadLogoBytes path every other
// branding consumer now shares.

import (
	"context"
	"strings"
	"testing"

	"github.com/hikrad/hikrad/internal/platform"
)

func TestHotspotPackageEmbedsConfiguredBranding(t *testing.T) {
	// platform.BrandingDir() resolves HIKRAD_BRANDING_DIR exactly once per
	// process (sync.Once) — set it before the first StoreLogo/ReadLogoBytes
	// call in this test binary so the logo never lands under the repo tree's
	// default data/branding path.
	t.Setenv("HIKRAD_BRANDING_DIR", t.TempDir())

	m := autoSetupTestModule(t)
	// autoSetupTestModule wires db/log/nas only (its own auto-setup tests
	// never touch settings); this test needs a real Settings service.
	m.settings = platform.NewSettings(m.db)
	ctx := context.Background()

	if err := m.settings.Set(ctx, "branding.name", "Tigris Net"); err != nil {
		t.Fatalf("set branding.name: %v", err)
	}
	if err := m.settings.Set(ctx, "branding.primary_color", "#e11d48"); err != nil {
		t.Fatalf("set branding.primary_color: %v", err)
	}

	png := mustPNGRadius(t)
	servedPath, _, err := platform.StoreLogo(png)
	if err != nil {
		t.Fatalf("StoreLogo: %v", err)
	}
	t.Cleanup(func() { _ = platform.DeleteLogo() })
	if err := m.settings.Set(ctx, "branding.logo_url", servedPath); err != nil {
		t.Fatalf("set branding.logo_url: %v", err)
	}

	b := m.loadHotspotBranding(ctx)
	if b.Name != "Tigris Net" {
		t.Errorf("loadHotspotBranding().Name = %q, want %q (the pre-phase bug always returned the hardcoded default here)", b.Name, "Tigris Net")
	}
	if b.ColorPrimary != "#e11d48" {
		t.Errorf("loadHotspotBranding().ColorPrimary = %q, want %q", b.ColorPrimary, "#e11d48")
	}
	if !strings.HasPrefix(b.LogoDataURI, "data:image/png;base64,") {
		t.Errorf("loadHotspotBranding().LogoDataURI = %q, want an inlined data: URI", b.LogoDataURI)
	}

	pkg, err := buildHotspotPackage(b, nil)
	if err != nil {
		t.Fatalf("buildHotspotPackage: %v", err)
	}
	files := unzip(t, pkg)
	login := files["login.html"]
	if !strings.Contains(login, "Tigris Net") {
		t.Error("generated login.html does not contain the configured instance name")
	}
	if !strings.Contains(login, "data:image/png;base64,") {
		t.Error("generated login.html does not inline the logo as a data: URI")
	}
	if strings.Contains(login, "/api/v1/branding/logo") {
		t.Error("generated login.html must never reference a fetchable branding URL — it must stay self-contained (NFR-7)")
	}
}

func mustPNGRadius(t *testing.T) []byte {
	t.Helper()
	// A minimal valid 1x1 PNG (magic bytes + IHDR/IDAT/IEND for a single
	// black pixel) — hand-built rather than importing image/png here, since
	// this package doesn't otherwise need it and the exact pixel content is
	// irrelevant to this test.
	return []byte{
		0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A,
		0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x52,
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x02, 0x00, 0x00, 0x00, 0x90, 0x77, 0x53,
		0xDE, 0x00, 0x00, 0x00, 0x0C, 0x49, 0x44, 0x41,
		0x54, 0x08, 0xD7, 0x63, 0xF8, 0xCF, 0xC0, 0x00,
		0x00, 0x03, 0x01, 0x01, 0x00, 0x18, 0xDD, 0x8D,
		0xB0, 0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4E,
		0x44, 0xAE, 0x42, 0x60, 0x82,
	}
}
