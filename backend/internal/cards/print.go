package cards

// Branded printing + per-card usage export (FR-137). No PDF-rendering
// dependency exists anywhere in this backend (receipts and the hotspot login
// package are both plain HTML/print-CSS, turned into an actual PDF by the
// OPERATOR'S OWN browser print dialog — the same "auto-sized, browser
// print → PDF" posture billing/receipt.go already uses), so this follows
// that already-established pattern rather than adding a new dependency.
//
// Printing takes the PLAINTEXT codes as request input rather than looking
// them up: codes are hashed at rest (identical to vouchers) and exist in
// plaintext ONLY in the generation response — the same "exists once" model
// vouchers' CSV download already follows. The panel calls this immediately
// after generation/add-more with the codes it just received; printing a
// batch again later means re-generating (add-more) rather than recovering
// lost plaintext, exactly like a lost voucher CSV.

import (
	"encoding/base64"
	"encoding/csv"
	"fmt"
	"html"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/hikrad/hikrad/internal/httpapi"
	"github.com/hikrad/hikrad/internal/platform"
)

type printRequest struct {
	Codes []string `json:"codes"`
}

// printGroupHandler renders the given codes as a branded, print-ready HTML
// page — a grid of cut-out cards, auto-sized via CSS grid rather than a
// fixed count per sheet — which the operator's browser turns into a PDF via
// its own print-to-PDF dialog (Ctrl+P), logo pulled from the same identity
// resolver the hotspot login page uses.
func (m *Module) printGroupHandler(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	g, err := getGroup(r.Context(), m.db, id)
	if err == errGroupNotFound {
		httpapi.Error(w, http.StatusNotFound, "not_found", "card group not found")
		return
	}
	if err != nil {
		m.internalError(w, "print group get", err)
		return
	}
	var in printRequest
	if !httpapi.Bind(w, r, &in) {
		return
	}
	if len(in.Codes) == 0 {
		httpapi.Error(w, http.StatusUnprocessableEntity, "validation_failed", "request validation failed",
			httpapi.FieldError{Field: "codes", Message: "at least one code is required"})
		return
	}

	logo, brandName := "", "HikRAD"
	if m.settings != nil {
		ident := platform.LoadIdentity(r.Context(), m.settings)
		if ident.Name != "" {
			brandName = ident.Name
		}
		if data, contentType, ok := platform.ReadLogoBytes(); ok {
			logo = "data:" + contentType + ";base64," + base64.StdEncoding.EncodeToString(data)
		}
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(buildPrintPage(brandName, logo, g, in.Codes)))
}

func buildPrintPage(brandName, logoDataURI string, g Group, codes []string) string {
	price := formatIQD(g.Price)
	var cardsHTML strings.Builder
	for _, code := range codes {
		cardsHTML.WriteString(fmt.Sprintf(`<div class="card">
  <div class="brand">%s</div>
  <div class="plan">%s%s</div>
  <div class="code">%s</div>
</div>
`, html.EscapeString(brandName), html.EscapeString(g.Name), price, html.EscapeString(code)))
	}
	logo := ""
	if logoDataURI != "" {
		logo = fmt.Sprintf(`<img src="%s" alt="%s" style="max-height:40px">`, html.EscapeString(logoDataURI), html.EscapeString(brandName))
	}
	return fmt.Sprintf(`<!doctype html>
<html><head><meta charset="utf-8"><title>%s — %s</title>
<style>
  * { box-sizing: border-box; }
  body { font-family: sans-serif; margin: 0; padding: 16px; }
  .header { display: flex; align-items: center; gap: 8px; margin-bottom: 12px; }
  .grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(220px, 1fr)); gap: 8px; }
  .card { border: 1px dashed #999; border-radius: 6px; padding: 10px; }
  .brand { font-weight: 700; font-size: 12px; }
  .plan { font-size: 11px; color: #555; }
  .code { font-family: monospace; font-size: 18px; letter-spacing: 1px; margin-top: 6px; }
  @media print { .card { break-inside: avoid; } }
</style></head>
<body>
  <div class="header">%s<h1 style="font-size:16px;margin:0">%s</h1></div>
  <div class="grid">
%s  </div>
</body></html>`, html.EscapeString(brandName), html.EscapeString(g.Name), logo, html.EscapeString(g.Name), cardsHTML.String())
}

func formatIQD(amount int64) string {
	if amount <= 0 {
		return ""
	}
	return " — " + strconv.FormatInt(amount, 10) + " IQD"
}

// usageCSVHandler exports per-card usage (FR-137: "per-card usage view" +
// "card traffic tracked distinctly") as CSV — the same export shape
// subscribers/bulk.go's bulkExport already establishes for lists.
func (m *Module) usageCSVHandler(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	list, err := listCards(r.Context(), m.db, id)
	if err != nil {
		m.internalError(w, "usage csv", err)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="card-usage-`+id+`.csv"`)
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"card_id", "state", "first_login_at", "bytes_down", "bytes_up", "uptime_seconds"})
	for _, c := range list {
		firstLogin := ""
		if c.FirstLoginAt != nil {
			firstLogin = c.FirstLoginAt.UTC().Format("2006-01-02T15:04:05Z")
		}
		_ = cw.Write([]string{
			c.ID, c.State, firstLogin,
			strconv.FormatInt(c.UsageDownBytes, 10), strconv.FormatInt(c.UsageUpBytes, 10),
			strconv.FormatInt(c.UptimeSeconds, 10),
		})
	}
	cw.Flush()
}
