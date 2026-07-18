package platform

// Logo storage tests (v2 phase 11, FR-91, contract C2). Pure filesystem —
// no database needed. TestMain pins HIKRAD_BRANDING_DIR to one temp
// directory for the whole test binary before any test runs, since BrandingDir
// resolves the env var exactly once (sync.Once) — setting it later, inside
// an individual test, would race with whichever test happens to call
// BrandingDir first.

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"strings"
	"testing"
)

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "hikrad-branding-test-")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)
	_ = os.Setenv("HIKRAD_BRANDING_DIR", dir)
	os.Exit(m.Run())
}

func mustPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: 200, G: 50, B: 50, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestStoreLogoValidation(t *testing.T) {
	t.Cleanup(func() { _ = DeleteLogo() })

	// Oversized (over the 1 MiB ceiling).
	oversized := make([]byte, maxLogoBytes+1)
	if _, _, err := StoreLogo(oversized); err == nil {
		t.Error("oversized upload should be rejected")
	}

	// Wrong type: an .exe (MZ header) "renamed" to .png — i.e. arbitrary
	// non-image bytes with no real magic number match, exactly the attack
	// AC-91b names.
	fakeExe := append([]byte("MZ\x90\x00\x03\x00\x00\x00"), bytes.Repeat([]byte{0x00}, 64)...)
	if _, _, err := StoreLogo(fakeExe); err == nil {
		t.Error("a non-image file should be rejected regardless of claimed extension")
	}

	// SVG containing <script> is rejected even though it's otherwise valid SVG.
	scriptSVG := []byte(`<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`)
	if _, _, err := StoreLogo(scriptSVG); err == nil {
		t.Error("an SVG containing <script> should be rejected")
	}

	// Valid PNG: 200 round-trips byte-identical via ReadLogoBytes.
	png1 := mustPNG(t, 4, 4)
	servedPath, contentType, err := StoreLogo(png1)
	if err != nil {
		t.Fatalf("valid PNG upload rejected: %v", err)
	}
	if contentType != "image/png" {
		t.Errorf("contentType = %q, want image/png", contentType)
	}
	if !strings.HasPrefix(servedPath, "/api/v1/branding/logo?v=") {
		t.Errorf("servedPath = %q, want a versioned /api/v1/branding/logo path", servedPath)
	}
	gotData, gotType, ok := ReadLogoBytes()
	if !ok || !bytes.Equal(gotData, png1) || gotType != "image/png" {
		t.Fatalf("ReadLogoBytes after PNG store: ok=%v type=%q len=%d, want the exact uploaded bytes", ok, gotType, len(gotData))
	}

	// Valid SVG (no script) round-trips too, and replaces the PNG (single
	// current asset — StoreLogo clears any prior file of a different ext).
	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="10" height="10"><rect width="10" height="10"/></svg>`)
	if _, ct, err := StoreLogo(svg); err != nil || ct != "image/svg+xml" {
		t.Fatalf("valid SVG upload: contentType=%q err=%v", ct, err)
	}
	gotData, gotType, ok = ReadLogoBytes()
	if !ok || !bytes.Equal(gotData, svg) || gotType != "image/svg+xml" {
		t.Fatalf("ReadLogoBytes after SVG store: ok=%v type=%q, want the SVG bytes (old PNG must be gone)", ok, gotType)
	}

	// Oversized raster dimensions.
	big := mustPNG(t, maxLogoDimenPx+1, 4)
	if len(big) <= maxLogoBytes { // guard: only meaningful if it didn't also trip the size ceiling
		if _, _, err := StoreLogo(big); err == nil {
			t.Error("a raster logo wider than 2048px should be rejected")
		}
	}
}

func TestDeleteLogoFallsBackCleanly(t *testing.T) {
	if _, _, err := StoreLogo(mustPNG(t, 2, 2)); err != nil {
		t.Fatalf("store: %v", err)
	}
	if _, _, ok := ReadLogoBytes(); !ok {
		t.Fatal("expected a logo to be present before delete")
	}
	if err := DeleteLogo(); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, _, ok := ReadLogoBytes(); ok {
		t.Error("ReadLogoBytes should report no logo after DeleteLogo")
	}
	// Deleting again (no file present) is not an error.
	if err := DeleteLogo(); err != nil {
		t.Errorf("DeleteLogo on an already-empty dir should not error, got: %v", err)
	}
}
