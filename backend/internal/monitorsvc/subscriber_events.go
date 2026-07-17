package monitorsvc

// Subscriber-facing event delivery (contract C7/C8, FR-55/FR-59): consumes
// D's billing.renewed and billing.card_payment Redis pub/sub events and
// delivers the WhatsApp + portal-push notifications. Fully independent of the
// admin alert engine — a dead WhatsApp/push endpoint here never touches
// Telegram/email/in-app and vice versa (delivery isolation, NFR-7). Built as
// a plain consumer rather than routed through the alert_rules engine because
// these are per-event subscriber messages, not admin-configured rules.

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
	ps := s.rdb.Subscribe(ctx, "billing.renewed", "billing.card_payment")
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
	case "billing.card_payment":
		s.handleCardPayment(ctx, payload)
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

// cardPaymentEvent mirrors D's publish shape (contract C8): "All decisions
// publish billing.card_payment {subscriber_id, state, reason?}".
type cardPaymentEvent struct {
	SubscriberID string `json:"subscriber_id"`
	State        string `json:"state"`
	Reason       string `json:"reason,omitempty"`
}

func (s *subscriberEvents) handleCardPayment(ctx context.Context, payload []byte) {
	var ev cardPaymentEvent
	if err := json.Unmarshal(payload, &ev); err != nil {
		s.log.Warn("subscriber-events: bad billing.card_payment payload", "error", err)
		return
	}
	contact, ok := loadSubscriberContact(ctx, s.db, ev.SubscriberID)
	if !ok {
		return
	}
	templateKey := "card_payment_" + ev.State // e.g. card_payment_approved | card_payment_rejected
	if contact.OptIn && contact.Phone != "" {
		params := []string{contact.Name, ev.Reason}
		if err := deliverSubscriberWhatsApp(ctx, s.settings, s.client, contact.Phone, contact.Language, templateKey, params); err != nil {
			s.log.Warn("subscriber-events: card_payment whatsapp failed", "subscriber_id", ev.SubscriberID, "state", ev.State, "error", err)
		}
	}
	if err := push.DeliverToSubscriber(ctx, ev.SubscriberID, push.Payload{
		TitleKey: "push.card_payment." + ev.State + ".title",
		BodyKey:  "push.card_payment." + ev.State + ".body",
		Params:   map[string]any{"state": ev.State, "reason": ev.Reason},
		URL:      "/",
	}); err != nil {
		s.log.Warn("subscriber-events: card_payment push failed", "subscriber_id", ev.SubscriberID, "error", err)
	}
}
