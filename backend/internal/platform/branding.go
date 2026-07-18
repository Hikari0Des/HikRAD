package platform

// Instance identity (v2 phase 11, FR-91, contracts C1/C2). Consolidates what
// were three independent, broken readers of the "branding" settings group
// (internal/portalapi's public endpoint, internal/radius/hotspot.go's
// captive-portal page, and internal/billing's receipt header — see
// docs/ops/known-issues.md) into one correct source: branding.name,
// branding.logo_url, branding.primary_color, branding.secondary_color are
// four independent settings keys (never a single "branding" key, which has
// never existed), read through LoadIdentity by every consumer.
//
// The logo itself lives on local disk under BrandingDir (NFR-7: never a
// database blob, never remote object storage) — same pattern
// internal/billing/ticket_attachments.go already established for payment
// proof uploads: env-var-with-sane-default directory resolution, magic-byte
// content sniffing (never the client's declared type), atomic write.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"net/http"
	"os"
	"path/filepath"
	"sync"
)

// Identity is the resolved instance identity every consumer renders.
type Identity struct {
	Name            string  // "" if unset — every consumer applies its own generic fallback
	LogoURL         *string // nil if no logo uploaded; a served path (e.g. "/api/v1/branding/logo?v=<hash>"), never a data: URI
	ThemeColor      *string // from branding.primary_color
	BackgroundColor *string // from branding.secondary_color
}

// LoadIdentity reads the four branding.* keys independently, tolerating any
// subset being absent. This is the ONLY place that should ever call
// Get[...](ctx, s, "branding.xxx") — every consumer calls this function
// instead of hand-rolling its own read (the root cause of the pre-phase bug:
// two callers read a single "branding" key that has never existed, and a
// third read a differently-shaped key entirely).
func LoadIdentity(ctx context.Context, s Settings) Identity {
	var id Identity
	if s == nil {
		return id
	}
	if v, err := Get[string](ctx, s, "branding.name"); err == nil {
		id.Name = v
	}
	if v, err := Get[string](ctx, s, "branding.logo_url"); err == nil && v != "" {
		id.LogoURL = &v
	}
	if v, err := Get[string](ctx, s, "branding.primary_color"); err == nil && v != "" {
		id.ThemeColor = &v
	}
	if v, err := Get[string](ctx, s, "branding.secondary_color"); err == nil && v != "" {
		id.BackgroundColor = &v
	}
	return id
}

const (
	maxLogoBytes    = 1 << 20 // 1 MiB
	maxLogoDimenPx  = 2048    // raster only; SVG is vector, no check
	logoSniffWindow = 1024    // bytes inspected for the SVG "<svg" heuristic
)

var (
	errLogoTooLarge       = errors.New("platform: logo exceeds the 1 MiB size limit")
	errLogoTypeNotAllowed = errors.New("platform: logo must be PNG, JPEG, or SVG")
	errLogoTooBig         = errors.New("platform: logo image dimensions exceed 2048x2048")
	errLogoSVGScript      = errors.New("platform: SVG logo may not contain a <script> element")
)

var (
	brandingDirOnce sync.Once
	brandingDir     string
)

// BrandingDir resolves the local-disk root for the instance logo
// (HIKRAD_BRANDING_DIR, defaulting to "data/branding" — same
// env-var-with-default pattern HIKRAD_PAYMENT_ATTACHMENTS_DIR/
// HIKRAD_ACCT_SPILL_DIR already establish).
func BrandingDir() string {
	brandingDirOnce.Do(func() {
		brandingDir = os.Getenv("HIKRAD_BRANDING_DIR")
		if brandingDir == "" {
			brandingDir = "data/branding"
		}
	})
	return brandingDir
}

// sniffLogoType returns the sniffed content type and file extension, or an
// error if data isn't a PNG/JPEG (via http.DetectContentType's magic-byte
// sniffer, never the client's declared Content-Type/filename) or a
// script-free SVG (Go's stdlib sniffer does not recognize SVG, since it's
// text/XML, so this is a dedicated check).
func sniffLogoType(data []byte) (contentType, ext string, err error) {
	sniffed := http.DetectContentType(data)
	switch sniffed {
	case "image/png":
		return "image/png", ".png", nil
	case "image/jpeg":
		return "image/jpeg", ".jpg", nil
	}
	if looksLikeSVG(data) {
		if bytes.Contains(bytes.ToLower(data), []byte("<script")) {
			return "", "", errLogoSVGScript
		}
		return "image/svg+xml", ".svg", nil
	}
	return "", "", fmt.Errorf("%w (detected %q)", errLogoTypeNotAllowed, sniffed)
}

// looksLikeSVG trims a UTF-8 BOM and leading whitespace/XML prolog, then
// checks for a "<svg" tag within the first logoSniffWindow bytes.
func looksLikeSVG(data []byte) bool {
	d := bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	d = bytes.TrimLeft(d, " \t\r\n")
	window := d
	if len(window) > logoSniffWindow {
		window = window[:logoSniffWindow]
	}
	return bytes.Contains(bytes.ToLower(window), []byte("<svg"))
}

// checkRasterDimensions decodes only the image header (no full decode) for
// PNG/JPEG and rejects anything wider or taller than maxLogoDimenPx.
func checkRasterDimensions(data []byte) error {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("platform: decode logo dimensions: %w", err)
	}
	if cfg.Width > maxLogoDimenPx || cfg.Height > maxLogoDimenPx {
		return errLogoTooBig
	}
	return nil
}

// StoreLogo validates data and writes it to <BrandingDir>/logo<ext>,
// replacing any previously stored logo (a single current asset, not a list —
// written via a temp file + rename so a crash mid-upload never leaves a
// half-written file being served). Returns the servedPath to store as
// branding.logo_url: a cache-busting content hash means every consumer that
// already just re-fetches GET /api/v1/branding gets a fresh URL the instant
// the logo changes, with no separate invalidation logic anywhere.
func StoreLogo(data []byte) (servedPath, contentType string, err error) {
	if len(data) == 0 {
		return "", "", errLogoTypeNotAllowed
	}
	if len(data) > maxLogoBytes {
		return "", "", errLogoTooLarge
	}
	contentType, ext, err := sniffLogoType(data)
	if err != nil {
		return "", "", err
	}
	if ext != ".svg" {
		if err := checkRasterDimensions(data); err != nil {
			return "", "", err
		}
	}

	dir := BrandingDir()
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", "", err
	}
	if err := clearStoredLogo(dir); err != nil {
		return "", "", err
	}
	finalPath := filepath.Join(dir, "logo"+ext)
	tmpPath := finalPath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o640); err != nil {
		return "", "", err
	}
	if err := os.Rename(tmpPath, finalPath); err != nil {
		_ = os.Remove(tmpPath)
		return "", "", err
	}

	sum := sha256.Sum256(data)
	servedPath = fmt.Sprintf("/api/v1/branding/logo?v=%s", hex.EncodeToString(sum[:])[:12])
	return servedPath, contentType, nil
}

// DeleteLogo removes the stored file, if any; not an error if none exists.
func DeleteLogo() error {
	return clearStoredLogo(BrandingDir())
}

// ReadLogoBytes reads the current logo straight off disk — used by the
// Hotspot captive-portal package builder, which must inline the raw bytes
// into a self-contained zip rather than fetch the served path (a router
// generating its login page must not depend on hikrad-api being reachable
// at build time — NFR-7).
func ReadLogoBytes() (data []byte, contentType string, ok bool) {
	path, ext, found := findStoredLogo(BrandingDir())
	if !found {
		return nil, "", false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "", false
	}
	return data, contentTypeForExt(ext), true
}

func contentTypeForExt(ext string) string {
	switch ext {
	case ".png":
		return "image/png"
	case ".jpg":
		return "image/jpeg"
	case ".svg":
		return "image/svg+xml"
	default:
		return "application/octet-stream"
	}
}

// findStoredLogo looks for logo.png / logo.jpg / logo.svg, in that order
// (at most one should ever exist — clearStoredLogo removes all three before
// every write — but checking all three keeps ReadLogoBytes/DeleteLogo
// correct even if a stale file from an interrupted operation lingers).
func findStoredLogo(dir string) (path, ext string, found bool) {
	for _, e := range []string{".png", ".jpg", ".svg"} {
		p := filepath.Join(dir, "logo"+e)
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p, e, true
		}
	}
	return "", "", false
}

func clearStoredLogo(dir string) error {
	for _, e := range []string{".png", ".jpg", ".svg"} {
		p := filepath.Join(dir, "logo"+e)
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}
