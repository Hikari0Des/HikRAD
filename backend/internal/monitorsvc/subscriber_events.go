package monitorsvc

// Subscriber + manager event delivery (contract C7/C8, FR-55/FR-59/FR-80):
// consumes billing's billing.renewed and billing.payment_ticket Redis pub/sub
// events and delivers WhatsApp + portal-push to the subscriber, and panel
// push to the owning manager (v2-2, FR-80.2: ticket submitted, or decided by
// someone other than the owner). Fully independent of the admin alert engine
// — a dead WhatsApp/push endpoint here never touches Telegram/email/in-app
// and vice versa (delivery isolation, NFR-7). Built as a plain consumer
// rather than routed through the alert_rules engine because these are
// per-event subscriber/manager messages, not admin-configured rules.
//
// The manager-facing half reuses push.DeliverToManager (panel Web Push) only
// — the existing "in-app notification center" (alert_events/NotificationsChannel)
// is a global admin broadcast with no per-manager scoping column, so treating
// it as this event's in-app channel would either leak to every admin or
// require a schema change outside this phase's frozen scope (C11 says "never
// a new channel"); documented here rather than silently narrowed.

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"time"

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
	client   *http.Client
}

func newSubscriberEvents(db *pgxpool.Pool, rdb *redis.Client, settings platform.Settings, log *slog.Logger) *subscriberEvents {
	return &subscriberEvents{db: db, rdb: rdb, settings: settings, log: log, client: httpClient()}
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
	contact, ok := loadSubscriberContact(ctx, s.db, ev.SubscriberID)
	if !ok {
		return
	}
	if contact.OptIn && contact.Phone != "" {
		params := []string{strconv.FormatInt(ev.Amount, 10), ev.ReceiptNo, ev.NewExpiresAt.Format("2006-01-02")}
		if err := deliverSubscriberWhatsApp(ctx, s.settings, s.client, contact.Phone, contact.Language, "payment_receipt", params); err != nil {
			s.log.Warn("subscriber-events: payment_receipt whatsapp failed", "subscriber_id", ev.SubscriberID, "error", err)
		}
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
	contact, ok := loadSubscriberContact(ctx, s.db, ev.SubscriberID)
	if !ok {
		return
	}
	templateKey := "payment_ticket_" + ev.State // e.g. payment_ticket_approved | payment_ticket_rejected
	if contact.OptIn && contact.Phone != "" {
		params := []string{contact.Name, ev.Reason}
		if err := deliverSubscriberWhatsApp(ctx, s.settings, s.client, contact.Phone, contact.Language, templateKey, params); err != nil {
			s.log.Warn("subscriber-events: payment_ticket whatsapp failed", "subscriber_id", ev.SubscriberID, "state", ev.State, "error", err)
		}
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
