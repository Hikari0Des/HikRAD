package portalapi

// Unit tests for GET /api/v1/branding's white_label field (FR-122, Decision
// 62). No DB needed: with m.settings == nil, brandingHandler skips
// platform.LoadIdentity entirely and only WhiteLabel is under test here —
// that field comes exclusively from platform.LicenseHasEntitlement, driven
// via the test-only snapshot seam.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hikrad/hikrad/internal/platform"
	"github.com/hikrad/hikrad/internal/platform/license"
)

func TestBrandingHandler_WhiteLabelReflectsEntitlement(t *testing.T) {
	m := &Module{}

	platform.SetCachedLicenseSnapshotForTest(platform.LicenseSnapshot{
		State:        license.StateValid,
		Entitlements: map[string]bool{},
	})
	t.Cleanup(func() { platform.SetCachedLicenseSnapshotForTest(platform.LicenseSnapshot{State: license.StateValid}) })

	req := httptest.NewRequest(http.MethodGet, "/api/v1/branding", nil)
	rec := httptest.NewRecorder()
	m.brandingHandler(rec, req)

	var resp brandingResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.WhiteLabel {
		t.Fatal("white_label = true without the entitlement granted")
	}

	platform.SetCachedLicenseSnapshotForTest(platform.LicenseSnapshot{
		State:        license.StateValid,
		Entitlements: map[string]bool{license.EntitlementWhiteLabel: true},
	})

	rec = httptest.NewRecorder()
	m.brandingHandler(rec, req)
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !resp.WhiteLabel {
		t.Fatal("white_label = false with the entitlement granted")
	}
}

func TestBrandingHandler_WhiteLabelDefaultsFalseWithNoLicense(t *testing.T) {
	m := &Module{}
	platform.SetCachedLicenseSnapshotForTest(platform.LicenseSnapshot{State: license.StateValid})
	t.Cleanup(func() { platform.SetCachedLicenseSnapshotForTest(platform.LicenseSnapshot{State: license.StateValid}) })

	req := httptest.NewRequest(http.MethodGet, "/api/v1/branding", nil)
	rec := httptest.NewRecorder()
	m.brandingHandler(rec, req)

	var resp brandingResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.WhiteLabel {
		t.Fatal("white_label defaulted true with no license installed — must fail closed (attribution stays visible)")
	}
}
