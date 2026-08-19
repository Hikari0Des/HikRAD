package notify

// The enqueue seam (FR-160/FR-161/FR-162): the ONLY way any HikRAD code
// puts a message in front of a subscriber, a staff channel, or a broadcast
// recipient. EnqueueEvent (subscriber-targeted, rule + template driven),
// EnqueueAlert (staff alert, rule-driven with per-rule recipient
// overrides) and EnqueueBroadcast (operator-composed, one-off) all funnel
// into insertOutbox/insertSkipped — nothing in this package, or in any
// caller, ever calls a channel sender directly (binding rule: one queue,
// no side channel).

import (
	"context"
	"encoding/json"

	"github.com/hikrad/hikrad/internal/platform"
	"github.com/jackc/pgx/v5/pgxpool"
)

// subscriberContact is the targeting data every subscriber-facing send
// needs: contact details plus the WhatsApp explicit-opt-in flag (FR-55.3 —
// a Meta compliance requirement, distinct from the generic opt-out model
// below, so it is honoured even under an "ignore opt-out" rule).
type subscriberContact struct {
	Name           string
	Phone          string
	Email          string
	Language       string
	WhatsAppOptIn  bool
	TelegramChatID string
}

func loadSubscriberContact(ctx context.Context, db *pgxpool.Pool, subscriberID string) (subscriberContact, bool) {
	var c subscriberContact
	err := db.QueryRow(ctx, `
		SELECT COALESCE(s.name,''), COALESCE(s.phone,''), COALESCE(s.email,''), COALESCE(s.language,'en'),
		       s.whatsapp_opt_in, COALESCE(tl.chat_id,'')
		  FROM subscribers s LEFT JOIN telegram_links tl ON tl.subscriber_id = s.id
		 WHERE s.id = $1::uuid`, subscriberID).
		Scan(&c.Name, &c.Phone, &c.Email, &c.Language, &c.WhatsAppOptIn, &c.TelegramChatID)
	if err != nil {
		return subscriberContact{}, false
	}
	return c, true
}

// channelOptedIn is the generic sms/email opt-out model (migration 0627):
// absence of a row means opted-in, matching the market norm this codebase
// already assumed before this phase had any record of consent at all.
func channelOptedIn(ctx context.Context, db *pgxpool.Pool, subscriberID, channel string) bool {
	var optedIn bool
	err := db.QueryRow(ctx,
		`SELECT opted_in FROM subscriber_channel_opt_ins WHERE subscriber_id = $1::uuid AND channel = $2`,
		subscriberID, channel).Scan(&optedIn)
	if err != nil {
		return true
	}
	return optedIn
}

// EnqueueInput is one subscriber-targeted event (renewal reminder, receipt,
// ticket-state change, …). Params feeds template placeholders on top of the
// contact's own name (always available as {{name}}).
type EnqueueInput struct {
	EventKey     string
	SubscriberID string
	Params       map[string]string
	Source       string // "" -> "rule"
	SourceID     string
}

// EnqueueEvent resolves the event's rule, the subscriber's contact details
// per channel, and the current template in the subscriber's own language,
// then queues one outbox row per rule-enabled channel — sent (queued),
// or skipped with the real reason (no contact address, opted out, no
// template) — gate item 1 (appears in the outbox either way) and gate item
// 2 (truthful reason) both apply to every row this produces.
//
// A disabled rule enqueues nothing at all — an operator who turns an event
// off expects total silence, not a queue full of skipped rows.
func EnqueueEvent(ctx context.Context, db *pgxpool.Pool, in EnqueueInput) ([]string, error) {
	rule, ok, err := getRule(ctx, db, in.EventKey)
	if err != nil {
		return nil, err
	}
	if !ok || !rule.Enabled {
		return nil, nil
	}
	contact, ok := loadSubscriberContact(ctx, db, in.SubscriberID)
	if !ok {
		return nil, nil
	}
	source := in.Source
	if source == "" {
		source = "rule"
	}
	params := map[string]string{"name": contact.Name}
	for k, v := range in.Params {
		params[k] = v
	}

	tpl, terr := currentTemplate(ctx, db, in.EventKey, contact.Language)

	var ids []string
	for _, chStr := range rule.Channels {
		ch := Channel(chStr)
		address, skip := resolveSubscriberChannel(ctx, db, contact, ch, in.SubscriberID, rule.IgnoreOptOut)

		var subject, body string
		var verPtr *int
		if terr == nil {
			subject = render(tpl.Subject, params)
			body = render(tpl.Body, params)
			v := tpl.Version
			verPtr = &v
		} else if skip == "" {
			skip = SkipNoTemplate
		}

		insert := OutboxInsert{
			Source: source, SourceID: in.SourceID, Event: in.EventKey, SubscriberID: in.SubscriberID,
			Address: address, Channel: ch, Locale: contact.Language, TemplateKey: in.EventKey,
			TemplateVersion: verPtr, Subject: subject, Body: body, IgnoreOptOut: rule.IgnoreOptOut,
		}
		var id string
		if skip != "" {
			id, err = insertSkipped(ctx, db, insert, skip)
		} else {
			id, err = insertOutbox(ctx, db, insert)
		}
		if err != nil {
			return ids, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// resolveSubscriberChannel returns the deliverable address for one channel,
// or a truthful skip reason when there isn't one. WhatsApp's explicit
// opt-in (FR-55.3) is checked unconditionally — ignoreOptOut governs only
// the generic sms/email opt-out model, never Meta's consent requirement.
func resolveSubscriberChannel(ctx context.Context, db *pgxpool.Pool, c subscriberContact, ch Channel, subscriberID string, ignoreOptOut bool) (address, skip string) {
	switch ch {
	case ChannelInApp:
		return "", ""
	case ChannelTelegram:
		if c.TelegramChatID == "" {
			return "", SkipNoContact
		}
		return c.TelegramChatID, ""
	case ChannelWhatsApp:
		if c.Phone == "" {
			return "", SkipNoContact
		}
		if !c.WhatsAppOptIn {
			return "", SkipOptedOut
		}
		return c.Phone, ""
	case ChannelSMS:
		if c.Phone == "" {
			return "", SkipNoContact
		}
		if !ignoreOptOut && !channelOptedIn(ctx, db, subscriberID, "sms") {
			return "", SkipOptedOut
		}
		return c.Phone, ""
	case ChannelEmail:
		if c.Email == "" {
			return "", SkipNoContact
		}
		if !ignoreOptOut && !channelOptedIn(ctx, db, subscriberID, "email") {
			return "", SkipOptedOut
		}
		return c.Email, ""
	default:
		return "", SkipChannelDisabled
	}
}

// AlertEnqueueInput is one staff-alert fire (monitorsvc's alert engine,
// FR-36). Recipients carries the same per-rule channel overrides
// alert_rules always has (telegram_chat_id / email_to / whatsapp_to);
// resolveAlertRecipients falls back to the channel's settings default.
type AlertEnqueueInput struct {
	RuleID     string
	RuleType   string
	Summary    string
	Channels   []string
	Recipients map[string]json.RawMessage
}

// EnqueueAlert queues one outbox row per (channel, resolved recipient) —
// a rule with three email recipients configured produces three rows, each
// individually truthful about its own delivery outcome. Staff alerts are
// never subject to subscriber opt-out (there is no subscriber on the row),
// so ignore_opt_out is always recorded true for provenance, not because it
// bypasses anything.
func EnqueueAlert(ctx context.Context, db *pgxpool.Pool, settings platform.Settings, in AlertEnqueueInput) error {
	for _, chStr := range in.Channels {
		ch := Channel(chStr)
		addresses := resolveAlertRecipients(ctx, settings, ch, in.Recipients)
		if len(addresses) == 0 {
			addresses = []string{""}
		}
		for _, addr := range addresses {
			insert := OutboxInsert{
				Source: "alert", SourceID: in.RuleID, Event: in.RuleType,
				Address: addr, Channel: ch, Locale: "en", TemplateKey: EventStaffAlert,
				Subject: "HikRAD alert", Body: in.Summary, IgnoreOptOut: true,
			}
			var err error
			if addr == "" && ch != ChannelInApp {
				_, err = insertSkipped(ctx, db, insert, SkipNoContact)
			} else {
				_, err = insertOutbox(ctx, db, insert)
			}
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func resolveAlertRecipients(ctx context.Context, settings platform.Settings, ch Channel, recipients map[string]json.RawMessage) []string {
	switch ch {
	case ChannelInApp:
		return []string{""}
	case ChannelTelegram:
		cfg, _ := platform.GetEncrypted[telegramConfig](ctx, settings, "notifications.telegram")
		addr := cfg.ChatID
		if raw, ok := recipients["telegram_chat_id"]; ok {
			var c string
			if json.Unmarshal(raw, &c) == nil && c != "" {
				addr = c
			}
		}
		if addr == "" {
			return nil
		}
		return []string{addr}
	case ChannelEmail:
		cfg, _ := platform.GetEncrypted[smtpConfig](ctx, settings, "notifications.smtp")
		to := cfg.To
		if raw, ok := recipients["email_to"]; ok {
			var override []string
			if json.Unmarshal(raw, &override) == nil && len(override) > 0 {
				to = override
			}
		}
		return to
	case ChannelWhatsApp:
		cfg, _ := platform.GetEncrypted[whatsAppConfig](ctx, settings, "notifications.whatsapp")
		to := cfg.To
		if raw, ok := recipients["whatsapp_to"]; ok {
			var override []string
			if json.Unmarshal(raw, &override) == nil && len(override) > 0 {
				to = override
			}
		}
		return to
	default:
		return nil
	}
}

// BroadcastInput is the Broadcast tab's one-off send (FR-163): an operator-
// composed message to an explicit subscriber set over one channel.
type BroadcastInput struct {
	SubscriberIDs []string
	Channel       Channel
	Subject       string
	Body          string
	CreatedBy     string
}

// EnqueueBroadcast queues one row per subscriber through the exact same
// path as any rule-driven event — the binding rule's "never a side
// channel" applies as much to a one-off broadcast as to anything else.
// Broadcast always honours opt-out (an operator composing a promotional
// blast has no "ignore opt-out" escape hatch — that flag exists only on
// genuinely transactional rules, and a broadcast is by definition not
// one).
func EnqueueBroadcast(ctx context.Context, db *pgxpool.Pool, in BroadcastInput) ([]string, error) {
	var ids []string
	for _, subscriberID := range in.SubscriberIDs {
		contact, ok := loadSubscriberContact(ctx, db, subscriberID)
		if !ok {
			continue
		}
		address, skip := resolveSubscriberChannel(ctx, db, contact, in.Channel, subscriberID, false)
		insert := OutboxInsert{
			Source: "broadcast", Event: "broadcast", SubscriberID: subscriberID,
			Address: address, Channel: in.Channel, Locale: contact.Language,
			Subject: in.Subject, Body: in.Body,
		}
		var id string
		var err error
		if skip != "" {
			id, err = insertSkipped(ctx, db, insert, skip)
		} else {
			id, err = insertOutbox(ctx, db, insert)
		}
		if err != nil {
			return ids, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}
