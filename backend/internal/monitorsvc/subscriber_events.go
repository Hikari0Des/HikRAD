package monitorsvc

// Subscriber + manager event delivery (contract C7/C8, FR-55/FR-59/FR-80):
// consumes billing's billing.renewed and billing.payment_ticket Redis pub/sub
// events and delivers to the subscriber and the owning manager (v2-2,
// FR-80.2: ticket submitted, or decided by someone other than the owner).
//
// v4-10: the subscriber-facing half no longer calls a WhatsApp sender
// directly — it enqueues into internal/notify's one shared queue
// (EventPaymentReceipt / EventPaymentTicketState), exactly like a broadcast
// or a staff alert (binding rule: one queue, no side channel). The
// manager-facing push half is untouched — Web Push to the panel is a
// distinct, pre-existing channel outside the v4-10 Notification Center's
// scope (same call this package's alert engine makes for FR-54.4), so it
// stays a direct push.DeliverToManager call.
//
// Fully independent of the admin alert engine — a dead channel here never
// touches Telegram/email/in-app and vice versa (delivery isolation, NFR-7).
// Built as a plain consumer rather than routed through the alert_rules
// engine because these are per-event subscriber/manager messages, not
// admin-configured rules.

import (
	"context"
	"encoding/json"
	"log/slog"
	"strconv"
	"time"

	"github.com/hikrad/hikrad/internal/notify"
	"github.com/hikrad/hikrad/internal/platform"
	"github.com/hikrad/hikrad/internal/push"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

type subscriberEvents struct {
	db       *pgxpool.Pool
	rdb      *redis.Client
	settings platform.Settings
	log      *slog.Logger
}

func newSubscriberEvents(db *pgxpool.Pool, rdb *redis.Client, settings platform.Settings, log *slog.Logger) *subscriberEvents {
	return &subscriberEvents{db: db, rdb: rdb, settings: settings, log: log}
}

// run subscribes until ctx ends, reconnecting on a short backoff if the
// subscription drops (Redis restart) — a gap here delays receipts/reminders,
// never blocks the renewal/decision itself (NFR-7).
func (s *subscriberEvents) run(ctx context.Context) {
	if s.rdb == nil {
		return
	}
	for ctx.Err() == nil {
		s.subscribeOnce(ctx)
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Second):
		}
	}
}

func (s *subscriberEvents) subscribeOnce(ctx context.Context) {
	ps := s.rdb.Subscribe(ctx, "billing.renewed", "billing.payment_ticket")
	defer ps.Close()
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-ps.Channel():
			if !ok {
				return
			}
			s.handle(ctx, msg.Channel, []byte(msg.Payload))
		}
	}
}

func (s *subscriberEvents) handle(ctx context.Context, channel string, payload []byte) {
	switch channel {
	case "billing.renewed":
		s.handleRenewed(ctx, payload)
	case "billing.payment_ticket":
		s.handlePaymentTicket(ctx, payload)
	}
}

// renewedEvent mirrors billing's publish shape exactly (contract C7; v2 phase
// 4 FR-69.1 renamed amount_iqd -> amount and added currency).
type renewedEvent struct {
	SubscriberID string    `json:"subscriber_id"`
	ReceiptNo    string    `json:"receipt_no"`
	Amount       int64     `json:"amount"`
	Currency     string    `json:"currency"`
	NewExpiresAt time.Time `json:"new_expires_at"`
	Source       string    `json:"source"`
}

func (s *subscriberEvents) handleRenewed(ctx context.Context, payload []byte) {
	var ev renewedEvent
	if err := json.Unmarshal(payload, &ev); err != nil {
		s.log.Warn("subscriber-events: bad billing.renewed payload", "error", err)
		return
	}
	if _, err := notify.EnqueueEvent(ctx, s.db, notify.EnqueueInput{
		EventKey:     notify.EventPaymentReceipt,
		SubscriberID: ev.SubscriberID,
		Source:       "rule",
		Params: map[string]string{
			"amount":     strconv.FormatInt(ev.Amount, 10),
			"currency":   ev.Currency,
			"receipt_no": ev.ReceiptNo,
			"expires_at": ev.NewExpiresAt.Format("2006-01-02"),
		},
	}); err != nil {
		s.log.Warn("subscriber-events: payment_receipt enqueue failed", "subscriber_id", ev.SubscriberID, "error", err)
	}
	if err := push.DeliverToSubscriber(ctx, ev.SubscriberID, push.Payload{
		TitleKey: "push.payment_receipt.title",
		BodyKey:  "push.payment_receipt.body",
		Params:   map[string]any{"amount": ev.Amount, "currency": ev.Currency, "receipt_no": ev.ReceiptNo},
		URL:      "/",
	}); err != nil {
		s.log.Warn("subscriber-events: payment_receipt push failed", "subscriber_id", ev.SubscriberID, "error", err)
	}
}

// paymentTicketEvent mirrors billing's ticket_notify.go publish shape
// exactly (v2-2, C11): every payment_tickets state change, plus who owns the
// subscriber and who decided it — carried at publish time so this consumer
// never needs a second DB round-trip to know who to notify.
type paymentTicketEvent struct {
	SubscriberID   string `json:"subscriber_id"`
	TicketID       string `json:"ticket_id"`
	State          string `json:"state"` // submitted | approved | rejected
	Reason         string `json:"reason,omitempty"`
	OwnerManagerID string `json:"owner_manager_id,omitempty"`
	DecidedBy      string `json:"decided_by,omitempty"`
}

func (s *subscriberEvents) handlePaymentTicket(ctx context.Context, payload []byte) {
	var ev paymentTicketEvent
	if err := json.Unmarshal(payload, &ev); err != nil {
		s.log.Warn("subscriber-events: bad billing.payment_ticket payload", "error", err)
		return
	}
	s.notifySubscriberTicket(ctx, ev)
	s.notifyManagerTicket(ctx, ev)
}

// notifySubscriberTicket is FR-80.1: the subscriber's own WhatsApp + portal
// push for every state (submitted/approved/rejected).
func (s *subscriberEvents) notifySubscriberTicket(ctx context.Context, ev paymentTicketEvent) {
	if _, err := notify.EnqueueEvent(ctx, s.db, notify.EnqueueInput{
		EventKey:     notify.EventPaymentTicketState,
		SubscriberID: ev.SubscriberID,
		Source:       "rule",
		Params:       map[string]string{"status": ev.State, "reason": ev.Reason},
	}); err != nil {
		s.log.Warn("subscriber-events: payment_ticket enqueue failed", "subscriber_id", ev.SubscriberID, "state", ev.State, "error", err)
	}
	if err := push.DeliverToSubscriber(ctx, ev.SubscriberID, push.Payload{
		TitleKey: "push.payment_ticket." + ev.State + ".title",
		BodyKey:  "push.payment_ticket." + ev.State + ".body",
		Params:   map[string]any{"state": ev.State, "reason": ev.Reason},
		URL:      "/",
	}); err != nil {
		s.log.Warn("subscriber-events: payment_ticket push failed", "subscriber_id", ev.SubscriberID, "error", err)
	}
}

// notifyManagerTicket is FR-80.2: the owning manager is pushed when a ticket
// lands in their queue (submitted), or when someone else decided it
// (decided_by set and different from the owner) — never on their own decision.
func (s *subscriberEvents) notifyManagerTicket(ctx context.Context, ev paymentTicketEvent) {
	if ev.OwnerManagerID == "" {
		return
	}
	var titleKey, bodyKey string
	switch {
	case ev.State == "submitted":
		titleKey, bodyKey = "push.payment_ticket_manager.submitted.title", "push.payment_ticket_manager.submitted.body"
	case ev.DecidedBy != "" && ev.DecidedBy != ev.OwnerManagerID:
		titleKey, bodyKey = "push.payment_ticket_manager.decided_elsewhere.title", "push.payment_ticket_manager.decided_elsewhere.body"
	default:
		return
	}
	if err := push.DeliverToManager(ctx, ev.OwnerManagerID, push.Payload{
		TitleKey: titleKey,
		BodyKey:  bodyKey,
		Params:   map[string]any{"state": ev.State, "ticket_id": ev.TicketID},
		URL:      "/billing/payment-tickets/" + ev.TicketID,
	}); err != nil {
		s.log.Warn("subscriber-events: payment_ticket manager push failed", "manager_id", ev.OwnerManagerID, "ticket_id", ev.TicketID, "error", err)
	}
}
