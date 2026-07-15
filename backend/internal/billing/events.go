package billing

// billing.renewed event (contract C7, FR-55): published on every completed
// renewal, from every source (panel, voucher, portal-<gateway>, card-trial,
// card-<type>), exactly once, after commit. C consumes it to send the
// WhatsApp payment_receipt template. Publish failure is logged and never
// blocks the renewal (NFR-7 posture) — the renewal has already committed by
// the time this runs.

import (
	"context"
	"encoding/json"
	"time"
)

type renewedEvent struct {
	SubscriberID string    `json:"subscriber_id"`
	ReceiptNo    string    `json:"receipt_no"`
	AmountIQD    int64     `json:"amount_iqd"`
	NewExpiresAt time.Time `json:"new_expires_at"`
	Source       string    `json:"source"`
}

func (m *Module) publishRenewed(ctx context.Context, subscriberID string, res renewResult, source string) {
	if m.rdb == nil {
		return
	}
	buf, err := json.Marshal(renewedEvent{
		SubscriberID: subscriberID,
		ReceiptNo:    res.ReceiptNo,
		AmountIQD:    res.priceIQD,
		NewExpiresAt: res.NewExpiresAt,
		Source:       source,
	})
	if err != nil {
		m.log.Error("billing: marshal billing.renewed event failed", "error", err)
		return
	}
	if err := m.rdb.Publish(ctx, "billing.renewed", buf).Err(); err != nil {
		m.log.Warn("billing: publish billing.renewed failed", "error", err)
	}
}
