package billing

// Printable receipts (FR-21). A payment renders to a self-contained, print-ready
// HTML page (A5 + thermal CSS, ISP branding, ar/en templates, Eastern-Arabic
// numerals per locale). Reprints NEVER create a ledger/payment entry — this is a
// pure read. A shareable unguessable token backs the no-auth /receipts/{token}
// link ("sendable" receipt, key flow 2 step 5).

import (
	"context"
	"errors"
	"fmt"
	"html"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/hikrad/hikrad/internal/httpapi"
	"github.com/jackc/pgx/v5"
)

// receiptData is everything a receipt template needs, loaded read-only.
type receiptData struct {
	ReceiptNo  string
	AmountIQD  int64
	Method     string
	At         time.Time
	Subscriber string
	Username   string
	Profile    string
}

func (m *Module) loadReceiptBy(ctx context.Context, column, value string) (receiptData, error) {
	var d receiptData
	err := m.db.QueryRow(ctx,
		`SELECT p.receipt_no, p.amount_iqd, p.method, p.at,
		        COALESCE(s.name, ''), COALESCE(s.username::text, ''), COALESCE(pr.name, '')
		   FROM payments p
		   LEFT JOIN subscribers s ON s.id = p.subscriber_id
		   LEFT JOIN profiles pr   ON pr.id = s.profile_id
		  WHERE p.`+column+` = $1`, value).
		Scan(&d.ReceiptNo, &d.AmountIQD, &d.Method, &d.At, &d.Subscriber, &d.Username, &d.Profile)
	if err != nil {
		return receiptData{}, err
	}
	d.At = d.At.UTC()
	return d, nil
}

// receiptHandler serves the authenticated receipt view by receipt number.
func (m *Module) receiptHandler(w http.ResponseWriter, r *http.Request) {
	d, err := m.loadReceiptBy(r.Context(), "receipt_no", chi.URLParam(r, "receipt_no"))
	if errors.Is(err, pgx.ErrNoRows) {
		httpapi.Error(w, http.StatusNotFound, "not_found", "receipt not found")
		return
	}
	if err != nil {
		m.internalError(w, "receipt", err)
		return
	}
	m.writeReceipt(w, r, d)
}

// sharedReceiptHandler serves the no-auth shareable receipt by token.
func (m *Module) sharedReceiptHandler(w http.ResponseWriter, r *http.Request) {
	d, err := m.loadReceiptBy(r.Context(), "share_token", chi.URLParam(r, "token"))
	if errors.Is(err, pgx.ErrNoRows) {
		http.Error(w, "receipt not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	m.writeReceipt(w, r, d)
}

func (m *Module) writeReceipt(w http.ResponseWriter, r *http.Request, d receiptData) {
	lang := r.URL.Query().Get("lang")
	if lang != "ar" && lang != "ku" && lang != "en" {
		lang = "en"
	}
	currency := m.getString(r.Context(), keyCurrency, "IQD")
	numeral := m.getString(r.Context(), keyReceiptNumeral, "auto")
	brand := m.branding(r.Context())
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(renderReceipt(d, lang, currency, numeral, brand)))
}

// receiptStrings holds the localized labels for a receipt template.
type receiptStrings struct {
	Title, Receipt, Date, Customer, Plan, Amount, Method, Thanks string
	RTL                                                          bool
}

func receiptLabels(lang string) receiptStrings {
	switch lang {
	case "ar":
		return receiptStrings{"إيصال", "رقم الإيصال", "التاريخ", "المشترك", "الباقة", "المبلغ", "طريقة الدفع", "شكراً لتعاملكم معنا", true}
	case "ku":
		return receiptStrings{"پسوڵە", "ژمارەی پسوڵە", "بەروار", "بەشداربوو", "پاکێج", "بڕ", "شێوازی پارەدان", "سوپاس بۆ مامەڵەکردنتان", true}
	default:
		return receiptStrings{"Receipt", "Receipt No", "Date", "Customer", "Plan", "Amount", "Method", "Thank you for your business", false}
	}
}

// renderReceipt produces a self-contained print-ready HTML receipt. Print CSS is
// sized for both A5 and 80mm thermal rolls; numbers/usernames stay LTR inside an
// RTL page. Eastern-Arabic numerals are used for ar/ku unless overridden.
func renderReceipt(d receiptData, lang, currency, numeral string, brand brandingConfig) string {
	l := receiptLabels(lang)
	useArabicNum := numeral == "arabic" || (numeral == "auto" && l.RTL)

	num := func(s string) string {
		if useArabicNum {
			return toEasternArabic(s)
		}
		return s
	}
	esc := html.EscapeString
	amount := num(formatThousands(d.AmountIQD)) + " " + esc(currency)
	dir := "ltr"
	align := "left"
	if l.RTL {
		dir, align = "rtl", "right"
	}
	brandName := brand.Name
	if brandName == "" {
		brandName = "HikRAD"
	}

	var b strings.Builder
	fmt.Fprintf(&b, `<!doctype html><html lang="%s" dir="%s"><head><meta charset="utf-8">`, esc(lang), dir)
	fmt.Fprintf(&b, `<meta name="viewport" content="width=device-width, initial-scale=1"><title>%s %s</title>`,
		esc(l.Receipt), esc(d.ReceiptNo))
	b.WriteString(`<style>
:root{color-scheme:light}
*{box-sizing:border-box}
body{font-family:-apple-system,'Segoe UI',Tahoma,Arial,sans-serif;margin:0;padding:16px;background:#f4f4f5;color:#111}
.receipt{max-width:420px;margin:0 auto;background:#fff;padding:20px;border:1px solid #ddd;border-radius:8px}
h1{font-size:18px;margin:0 0 4px;text-align:center}
.sub{text-align:center;color:#666;font-size:12px;margin-bottom:12px}
table{width:100%;border-collapse:collapse;font-size:14px}
td{padding:6px 0;border-bottom:1px dashed #e5e5e5}
td.k{color:#666}
td.v{text-align:` + align + `;font-weight:600}
.mono{direction:ltr;unicode-bidi:isolate;font-variant-numeric:tabular-nums}
.total{font-size:18px}
.thanks{text-align:center;color:#666;font-size:12px;margin-top:14px}
@media print{body{background:#fff;padding:0}.receipt{border:none;max-width:80mm}}
</style></head><body>`)
	fmt.Fprintf(&b, `<div class="receipt"><h1>%s</h1><div class="sub">%s`, esc(brandName), esc(l.Title))
	if brand.Phone != "" {
		fmt.Fprintf(&b, ` · <span class="mono">%s</span>`, esc(brand.Phone))
	}
	b.WriteString(`</div><table>`)
	row := func(k, v string, mono, total bool) {
		cls := "v"
		if mono {
			cls += " mono"
		}
		if total {
			cls += " total"
		}
		fmt.Fprintf(&b, `<tr><td class="k">%s</td><td class="%s">%s</td></tr>`, esc(k), cls, v)
	}
	row(l.Receipt, `<span class="mono">`+esc(d.ReceiptNo)+`</span>`, false, false)
	row(l.Date, num(d.At.Format("2006-01-02 15:04")), false, false)
	if d.Subscriber != "" || d.Username != "" {
		who := d.Subscriber
		if who == "" {
			who = d.Username
		}
		row(l.Customer, esc(who), false, false)
	}
	if d.Profile != "" {
		row(l.Plan, esc(d.Profile), false, false)
	}
	row(l.Method, esc(d.Method), false, false)
	row(l.Amount, amount, false, true)
	fmt.Fprintf(&b, `</table><div class="thanks">%s</div></div></body></html>`, esc(l.Thanks))
	return b.String()
}

// toEasternArabic maps ASCII digits to Eastern-Arabic numerals (U+0660..0669).
func toEasternArabic(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune('٠' + (r - '0'))
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// formatThousands renders an integer amount with thousands separators.
func formatThousands(n int64) string {
	neg := n < 0
	if neg {
		n = -n
	}
	s := fmt.Sprintf("%d", n)
	var parts []string
	for len(s) > 3 {
		parts = append([]string{s[len(s)-3:]}, parts...)
		s = s[:len(s)-3]
	}
	parts = append([]string{s}, parts...)
	out := strings.Join(parts, ",")
	if neg {
		out = "-" + out
	}
	return out
}
