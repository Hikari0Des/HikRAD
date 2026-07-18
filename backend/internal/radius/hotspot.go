package radius

// Hotspot login-page package endpoint (FR-18): GET /api/v1/nas/{id}/hotspot-package
// returns a zip of the MikroTik Hotspot login.html + assets, themed from the
// Branding settings (01/FR-53) and carrying username/password AND voucher-code
// login. E's NAS screen exposes it as a download button. The package is
// self-contained (assets inline / served by the router) so it needs no external
// asset hosts; the walled-garden hosts the page's flow relies on (portal /
// payment / expired-redirect) are the same ones the FR-14 config snippet already
// emits — the package README restates them so the operator can confirm coverage
// (FR-18.2).

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/hikrad/hikrad/internal/httpapi"
	"github.com/hikrad/hikrad/internal/platform"
	"github.com/jackc/pgx/v5"
)

// hotspotBranding is the slice of the instance identity (v2 phase 11, FR-91)
// the login page renders. Read through platform.LoadIdentity/ReadLogoBytes —
// the single corrected branding source (see docs/ops/known-issues.md: this
// used to read a settings key, "branding", that has never existed, so this
// page always rendered the hardcoded default on every install). The logo is
// inlined as a base64 data: URI from the raw on-disk bytes, never a fetchable
// URL — the generated page must stay self-contained on the router with zero
// runtime dependency on hikrad-api being reachable (NFR-7).
type hotspotBranding struct {
	Name         string
	ColorPrimary string
	LogoDataURI  string
}

func (b hotspotBranding) withDefaults() hotspotBranding {
	if strings.TrimSpace(b.Name) == "" {
		b.Name = "HikRAD Wi-Fi"
	}
	if strings.TrimSpace(b.ColorPrimary) == "" {
		b.ColorPrimary = "#2563eb"
	}
	return b
}

// loadHotspotBranding reads the current instance identity, tolerating an
// unset settings service or no configured logo.
func (m *module) loadHotspotBranding(ctx context.Context) hotspotBranding {
	if m.settings == nil {
		return hotspotBranding{}.withDefaults()
	}
	id := platform.LoadIdentity(ctx, m.settings)
	b := hotspotBranding{Name: id.Name}
	if id.ThemeColor != nil {
		b.ColorPrimary = *id.ThemeColor
	}
	if data, contentType, ok := platform.ReadLogoBytes(); ok {
		b.LogoDataURI = "data:" + contentType + ";base64," + base64.StdEncoding.EncodeToString(data)
	}
	return b.withDefaults()
}

func (m *module) hotspotPackageHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	n, err := getNAS(ctx, m.db, chi.URLParam(r, "id"))
	if errors.Is(err, pgx.ErrNoRows) {
		httpapi.Error(w, http.StatusNotFound, "not_found", "nas not found")
		return
	}
	if err != nil {
		m.internal(w, "get nas for hotspot package", err)
		return
	}
	// FR-62: a NAS no longer *is* a hotspot — it may run one among several
	// services. The login package is offered as soon as any enabled hotspot
	// instance exists, which is also true of the multi-service routers v1's
	// nas.type='hotspot' check would have wrongly refused.
	services, err := enabledServices(ctx, m.db, n.ID)
	if err != nil {
		m.internal(w, "list nas services for hotspot package", err)
		return
	}
	hasHotspot := false
	for _, s := range services {
		if s.Service == "hotspot" {
			hasHotspot = true
			break
		}
	}
	if !hasHotspot {
		httpapi.Error(w, http.StatusBadRequest, "not_hotspot",
			"hotspot login package is only available for a NAS with an enabled hotspot service")
		return
	}

	branding := m.loadHotspotBranding(ctx)
	pkg, err := buildHotspotPackage(branding, defaultWalledGarden())
	if err != nil {
		m.internal(w, "build hotspot package", err)
		return
	}

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="hotspot-`+safeFilename(n.Name)+`.zip"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(pkg)
}

// buildHotspotPackage renders the themed assets and zips them. The zip contains
// login.html, style.css, md5.js and README.txt.
func buildHotspotPackage(b hotspotBranding, walledGarden []string) ([]byte, error) {
	logo := `<div style="font-weight:700;font-size:28px;color:var(--primary,#2563eb)">` + htmlEscape(b.Name) + `</div>`
	if b.LogoDataURI != "" {
		logo = `<img src="` + htmlEscape(b.LogoDataURI) + `" alt="` + htmlEscape(b.Name) + `">`
	}
	rep := strings.NewReplacer(
		"__HikRAD_NAME__", htmlEscape(b.Name),
		"__HikRAD_PRIMARY__", cssColor(b.ColorPrimary),
		"__HikRAD_LOGO__", logo,
	)
	login := rep.Replace(hotspotLoginHTML)
	style := rep.Replace(hotspotStyleCSS)

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	files := []struct{ name, body string }{
		{"login.html", login},
		{"style.css", style},
		{"md5.js", hotspotMD5JS},
		{"README.txt", hotspotReadme(b, walledGarden)},
	}
	for _, f := range files {
		fw, err := zw.Create(f.name)
		if err != nil {
			return nil, err
		}
		if _, err := fw.Write([]byte(f.body)); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func hotspotReadme(b hotspotBranding, walledGarden []string) string {
	var sb strings.Builder
	sb.WriteString(b.Name + " — MikroTik Hotspot login package\n")
	sb.WriteString("=================================================\n\n")
	sb.WriteString("Contents: login.html, style.css, md5.js.\n\n")
	sb.WriteString("Install\n-------\n")
	sb.WriteString("1. Upload these files to the router's hotspot directory\n")
	sb.WriteString("   (Files list, or FTP to /hotspot/ ; keep the filenames).\n")
	sb.WriteString("2. Point the hotspot profile's html-directory at that folder.\n")
	sb.WriteString("3. Enable login-by http-chap AND http-pap on the hotspot profile\n")
	sb.WriteString("   (the page hashes the password with md5.js when CHAP is offered,\n")
	sb.WriteString("   and falls back to PAP otherwise).\n\n")
	sb.WriteString("Voucher login\n-------------\n")
	sb.WriteString("The 'Voucher' tab submits the code as both username and password;\n")
	sb.WriteString("HikRAD detects the voucher format and redeems it at auth time.\n\n")
	sb.WriteString("Walled-garden hosts (allow before login)\n")
	sb.WriteString("----------------------------------------\n")
	if len(walledGarden) == 0 {
		sb.WriteString("(none configured — set HIKRAD_PORTAL_HOSTS if the page loads remote\n")
		sb.WriteString(" assets or redirects to a portal/payment host.)\n")
	} else {
		for _, h := range walledGarden {
			sb.WriteString("  /ip hotspot walled-garden add dst-host=" + h + " action=allow\n")
		}
	}
	sb.WriteString("\nThese match the entries in the NAS config snippet (FR-14).\n")
	return sb.String()
}

// safeFilename keeps a NAS name safe for a Content-Disposition filename.
func safeFilename(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "nas"
	}
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r == '-' || r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	return b.String()
}

// cssColor sanitizes a branding colour so it cannot break out of the CSS custom
// property context. Allows #hex and simple rgb()/named tokens.
func cssColor(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "#2563eb"
	}
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z':
		case r == '#' || r == '(' || r == ')' || r == ',' || r == '.' || r == ' ' || r == '%':
		default:
			return "#2563eb" // reject anything with CSS-injection potential
		}
	}
	return s
}

// htmlEscape is a minimal HTML-attribute/text escaper (avoids importing
// html/template just to escape a couple of branding strings).
func htmlEscape(s string) string {
	r := strings.NewReplacer(
		"&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&#39;",
	)
	return r.Replace(s)
}
