package billing

// Portal invoice surface (v4-11, FR-171, consuming v4-9's FR-154 invoices).
// Subscriber-scoped read-only seam for portalapi — same shape/discipline as
// portal_seam.go's PortalPayments: identity comes from the caller
// (portalapi resolves it from the token, IDOR rule), this package only ever
// filters by the exact subscriber_id it's given, never trusts a caller-
// supplied id against another subscriber's rows.
//
// "PDF": this codebase has no PDF-generation library anywhere (receipt.go's
// own "printable receipt" is a self-contained, print-ready HTML page, not a
// real PDF) — PortalInvoicePrintHTML follows the identical, already-
// established convention rather than introducing a new dependency for one
// screen.

import (
	"context"
	"errors"
	"fmt"
	"html"
	"strings"

	"github.com/jackc/pgx/v5"
)

var ErrInvoiceNotFound = errors.New("billing: invoice not found")

// PortalInvoices returns subscriberID's own invoices, newest first.
func PortalInvoices(ctx context.Context, subscriberID string) ([]invoiceView, error) {
	if singleton == nil {
		return nil, ErrNotReady
	}
	rows, err := singleton.db.Query(ctx,
		`SELECT `+invoiceColumns+` FROM invoices i WHERE i.subscriber_id = $1::uuid
		  ORDER BY i.issue_date DESC, i.id DESC LIMIT 100`, subscriberID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []invoiceView{}
	for rows.Next() {
		v, err := scanInvoice(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, v)
	}
	return items, rows.Err()
}

// PortalInvoice returns one invoice + its lines, but ONLY when it belongs to
// subscriberID — the IDOR check portalapi relies on (never trust an id alone).
func PortalInvoice(ctx context.Context, subscriberID, invoiceID string) (invoiceView, error) {
	if singleton == nil {
		return invoiceView{}, ErrNotReady
	}
	v, err := scanInvoice(singleton.db.QueryRow(ctx,
		`SELECT `+invoiceColumns+` FROM invoices i WHERE i.id = $1::uuid AND i.subscriber_id = $2::uuid`,
		invoiceID, subscriberID))
	if errors.Is(err, pgx.ErrNoRows) {
		return invoiceView{}, ErrInvoiceNotFound
	}
	if err != nil {
		return invoiceView{}, err
	}
	rows, err := singleton.db.Query(ctx,
		`SELECT description, category, quantity, unit_price, amount FROM invoice_lines WHERE invoice_id = $1::uuid`, invoiceID)
	if err != nil {
		return invoiceView{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var l invoiceLineView
		if err := rows.Scan(&l.Description, &l.Category, &l.Quantity, &l.UnitPrice, &l.Amount); err != nil {
			return invoiceView{}, err
		}
		v.Lines = append(v.Lines, l)
	}
	return v, rows.Err()
}

// PortalInvoicePrintHTML renders a self-contained, print-ready HTML page for
// one of subscriberID's own invoices (the FR-171 "PDF" — see this file's
// doc comment) — the browser's own print-to-PDF is the delivery mechanism,
// identical in spirit to receipt.go's existing printable receipt.
func PortalInvoicePrintHTML(ctx context.Context, subscriberID, invoiceID string) (string, error) {
	v, err := PortalInvoice(ctx, subscriberID, invoiceID)
	if err != nil {
		return "", err
	}
	var lines strings.Builder
	for _, l := range v.Lines {
		fmt.Fprintf(&lines, `<tr><td>%s</td><td>%.2f</td><td>%d</td><td>%d</td></tr>`,
			html.EscapeString(l.Description), l.Quantity, l.UnitPrice, l.Amount)
	}
	return fmt.Sprintf(`<!doctype html><html><head><meta charset="utf-8">
<title>%s</title>
<style>
  body{font-family:sans-serif;max-width:640px;margin:2rem auto;color:#111}
  table{width:100%%;border-collapse:collapse;margin-top:1rem}
  td,th{padding:.4rem;border-bottom:1px solid #ddd;text-align:start}
  .total{font-weight:bold}
  @media print{body{margin:0}}
</style></head><body>
<h1>%s</h1>
<p>%s — %s</p>
<table>
  <thead><tr><th>Description</th><th>Qty</th><th>Unit</th><th>Amount</th></tr></thead>
  <tbody>%s</tbody>
</table>
<p class="total">Total: %d — Status: %s</p>
</body></html>`,
		html.EscapeString(v.InvoiceNo), html.EscapeString(v.InvoiceNo), v.IssueDate, v.DueDate,
		lines.String(), v.Total, html.EscapeString(v.Status)), nil
}
