package billing

// Portal payment history (C2 FR-41.3). Unrelated to the gateway surface
// removed in v2-2 (C12) — this reads the `payments` table (the
// customer-facing gross record every renewal source, ticket-approval
// included, already writes), never the removed payment_intents/gateway
// machinery.

import (
	"context"
	"time"

	"github.com/hikrad/hikrad/internal/httpapi"
)

// portalPaymentSummary is one row of GET /portal/payments (own ledger slice,
// C2 FR-41.3) — the customer-facing gross payment record, not the internal
// balance-movement ledger. id/type/reference are not frozen by C2 (only the
// route is); this shape mirrors F's already-written client exactly (see
// frontend/portal/src/api/usage.ts) rather than inventing a divergent one.
type portalPaymentSummary struct {
	ID        string    `json:"id"`
	At        time.Time `json:"at"`
	Type      string    `json:"type"` // payments.method: renewal|voucher_redeem|ticket-<method>|refund
	Amount    int64     `json:"amount"`
	Currency  string    `json:"currency"`
	Source    string    `json:"source"`
	Reference string    `json:"reference"`
}

func (m *Module) portalPayments(ctx context.Context, subscriberID string, page httpapi.PageRequest) ([]portalPaymentSummary, string, error) {
	var cursorTS *time.Time
	var cursorNo *string
	if len(page.Cursor) == 2 {
		t, terr := time.Parse(time.RFC3339Nano, page.Cursor[0])
		if terr != nil {
			return nil, "", httpapi.ErrBadCursor
		}
		cursorTS, cursorNo = &t, &page.Cursor[1]
	} else if page.Cursor != nil {
		return nil, "", httpapi.ErrBadCursor
	}
	rows, err := m.db.Query(ctx,
		`SELECT pay.receipt_no, pay.at, pay.method, pay.amount, pay.currency, pay.source, COALESCE(l.reference,'')
		   FROM payments pay
		   LEFT JOIN ledger_transactions l ON l.id = pay.ledger_tx_id
		  WHERE pay.subscriber_id = $1::uuid
		    AND ($2::timestamptz IS NULL OR (pay.at, pay.receipt_no) < ($2::timestamptz, $3))
		  ORDER BY pay.at DESC, pay.receipt_no DESC
		  LIMIT $4`,
		subscriberID, cursorTS, cursorNo, page.Limit+1)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	items := make([]portalPaymentSummary, 0, page.Limit)
	for rows.Next() {
		var s portalPaymentSummary
		if err := rows.Scan(&s.ID, &s.At, &s.Type, &s.Amount, &s.Currency, &s.Source, &s.Reference); err != nil {
			return nil, "", err
		}
		s.At = s.At.UTC()
		items = append(items, s)
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}
	next := ""
	if len(items) > page.Limit {
		items = items[:page.Limit]
		last := items[len(items)-1]
		next = httpapi.EncodeCursor(last.At.Format(time.RFC3339Nano), last.ID)
	}
	return items, next, nil
}
