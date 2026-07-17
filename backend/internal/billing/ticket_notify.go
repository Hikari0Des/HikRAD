package billing

// Notification publish (v2-2, FR-80, contract C11). Every state-changing
// operation publishes billing.payment_ticket (renamed from Phase 4's
// billing.card_payment — same consumer package, monitorsvc, extended to
// also branch on decided_by != owner for the manager-facing half). This is
// the ONLY publish point — every notification in FR-80's matrix traces back
// to one of these calls, never a second invented message.

import (
	"context"
	"encoding/json"
)

// paymentTicketEvent is the wire shape monitorsvc's subscriber_events.go
// consumes (both the subscriber-facing and manager-facing halves). Carries
// OwnerManagerID/DecidedBy directly (resolved here, at publish time) so the
// consumer never needs a second DB round-trip to know who to notify.
type paymentTicketEvent struct {
	SubscriberID   string `json:"subscriber_id"`
	TicketID       string `json:"ticket_id"`
	State          string `json:"state"` // submitted | approved | rejected
	Reason         string `json:"reason,omitempty"`
	OwnerManagerID string `json:"owner_manager_id,omitempty"`
	DecidedBy      string `json:"decided_by,omitempty"`
}

// publishTicketEvent fires billing.payment_ticket for C's subscriber+manager
// notification delivery (FR-80). Publish failure is logged, never blocks the
// decision itself (NFR-7). decidedBy is "" for a "submitted" event.
func (m *Module) publishTicketEvent(ctx context.Context, subscriberID, ticketID, state, reason string) {
	m.publishTicketEventDecided(ctx, subscriberID, ticketID, state, reason, "")
}

func (m *Module) publishTicketEventDecided(ctx context.Context, subscriberID, ticketID, state, reason, decidedBy string) {
	if m.rdb == nil {
		return
	}
	var ownerID *string
	_ = m.db.QueryRow(ctx, `SELECT owner_manager_id::text FROM subscribers WHERE id = $1::uuid`, subscriberID).Scan(&ownerID)
	buf, err := json.Marshal(paymentTicketEvent{
		SubscriberID: subscriberID, TicketID: ticketID, State: state, Reason: reason,
		OwnerManagerID: strOr(ownerID, ""), DecidedBy: decidedBy,
	})
	if err != nil {
		m.log.Error("billing: marshal billing.payment_ticket event failed", "error", err)
		return
	}
	if err := m.rdb.Publish(ctx, "billing.payment_ticket", buf).Err(); err != nil {
		m.log.Warn("billing: publish billing.payment_ticket failed", "error", err)
	}
}
