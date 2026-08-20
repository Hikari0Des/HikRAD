package notify

// EnqueuePasscode (v4-11, FR-169/FR-172) is the passcode-delivery seam for
// self-service guest registration and OTP login. It cannot go through
// EnqueueEvent: that function's whole targeting model is "look up an
// EXISTING subscriber's contact/opt-in state" (loadSubscriberContact), but a
// registration passcode is sent to a phone number BEFORE any subscriber row
// exists. This reuses EnqueueEvent's other two pieces — the rule (so an
// operator can enable/disable and choose channels exactly like every other
// event) and the current template (trilingual, versioned) — and writes
// straight to the raw phone address, skipping only the subscriber-contact
// lookup and the opt-out model (there is no subscriber to opt out yet, and
// FR-169's rule is seeded ignore_opt_out=true — a passcode is the one
// message a visitor explicitly just asked for).
//
// Frozen contract (phase brief §4): "Passcode issuance goes through v4-10's
// queue like every other message" — insertOutbox/insertSkipped are the ONLY
// two functions in this package that ever write a notification_outbox row,
// and this function calls no other write path.

import (
	"context"
	"strconv"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	EventGuestRegistrationPasscode = "guest_registration_passcode"
	EventOTPLoginPasscode          = "otp_login_passcode"
)

// PasscodeInput is one passcode send. SubscriberID is "" for a registration
// passcode (no account exists yet) and set for an OTP login passcode.
type PasscodeInput struct {
	EventKey     string // EventGuestRegistrationPasscode | EventOTPLoginPasscode
	Phone        string
	SubscriberID string
	Locale       string
	Code         string
	TTLMinutes   int
}

// EnqueuePasscodeResult reports which channel actually got queued (or why
// none did) — portalapi's registration/OTP handlers use this to give an
// honest "we couldn't send you anything" response instead of a false
// "check your phone" when no channel is configured/enabled at all.
type EnqueuePasscodeResult struct {
	Queued  bool
	Channel Channel
	Skip    string // populated when Queued is false but a row was still written (channel disabled, no template)
}

// EnqueuePasscode picks the rule's first channel with a usable address
// (SMS/WhatsApp both use the phone number directly — there is no separate
// "contact record" to resolve) and queues exactly one outbox row through it.
// A rule with multiple channels configured (e.g. ["sms","whatsapp"]) tries
// them in order and queues the first — a passcode is a single message to
// one channel, unlike a broadcast/event fan-out, so this deliberately does
// NOT queue one row per channel.
func EnqueuePasscode(ctx context.Context, db *pgxpool.Pool, in PasscodeInput) (EnqueuePasscodeResult, error) {
	rule, ok, err := getRule(ctx, db, in.EventKey)
	if err != nil {
		return EnqueuePasscodeResult{}, err
	}
	if !ok || !rule.Enabled || len(rule.Channels) == 0 {
		return EnqueuePasscodeResult{Skip: SkipRuleDisabled}, nil
	}

	locale := in.Locale
	if locale == "" {
		locale = "en"
	}
	tpl, terr := currentTemplate(ctx, db, in.EventKey, locale)

	var chosen Channel
	for _, chStr := range rule.Channels {
		ch := Channel(chStr)
		if ch == ChannelSMS || ch == ChannelWhatsApp {
			chosen = ch
			break
		}
	}
	if chosen == "" {
		return EnqueuePasscodeResult{Skip: SkipChannelDisabled}, nil
	}

	params := map[string]string{"code": in.Code}
	if in.TTLMinutes > 0 {
		params["ttl_minutes"] = strconv.Itoa(in.TTLMinutes)
	}
	var subject, body string
	var verPtr *int
	skip := ""
	if terr == nil {
		subject = render(tpl.Subject, params)
		body = render(tpl.Body, params)
		v := tpl.Version
		verPtr = &v
	} else {
		skip = SkipNoTemplate
	}

	insert := OutboxInsert{
		Source: "guest_passcode", Event: in.EventKey, SubscriberID: in.SubscriberID,
		Address: in.Phone, Channel: chosen, Locale: locale, TemplateKey: in.EventKey,
		TemplateVersion: verPtr, Subject: subject, Body: body, IgnoreOptOut: rule.IgnoreOptOut,
	}
	var id string
	if skip != "" {
		id, err = insertSkipped(ctx, db, insert, skip)
	} else {
		id, err = insertOutbox(ctx, db, insert)
	}
	if err != nil {
		return EnqueuePasscodeResult{}, err
	}
	_ = id
	return EnqueuePasscodeResult{Queued: skip == "", Channel: chosen, Skip: skip}, nil
}
