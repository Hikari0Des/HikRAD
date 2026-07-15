package monitorsvc

// Subscriber-facing WhatsApp template sending (FR-55, contract C7). Distinct
// from the admin-alert whatsAppSender in channels.go: these messages carry
// several typed body parameters (amount, receipt number, days left…) rather
// than one free-text summary, and the recipient/template/language are
// resolved per subscriber rather than from a fixed rule config. Credentials
// (token/phone_id) are still the one "notifications.whatsapp" setting group
// (FR-53.2) — only the template catalog is a new key.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/hikrad/hikrad/internal/platform"
	"github.com/jackc/pgx/v5/pgxpool"
)

// subscriberTemplatesKey holds the FR-55.1 v1 template catalog: approved Meta
// template name per (template key, language). Configured in settings
// (FR-53.2) rather than hardcoded, since template names are assigned at Meta
// onboarding time and vary per business account.
const subscriberTemplatesKey = "notifications.whatsapp_templates"

// templateCatalog is keyed template_key -> lang -> approved template name,
// e.g. templates["payment_receipt"]["ar"] = "payment_receipt_ar".
type templateCatalog struct {
	Templates map[string]map[string]string `json:"templates"`
}

// resolveTemplate looks up the approved template name for key+lang, falling
// back to English if the subscriber's language has no registered template
// (never silently drops the message over a missing translation).
func resolveTemplate(cat templateCatalog, key, lang string) (string, bool) {
	byLang, ok := cat.Templates[key]
	if !ok {
		return "", false
	}
	if name, ok := byLang[lang]; ok && name != "" {
		return name, true
	}
	if name, ok := byLang["en"]; ok && name != "" {
		return name, true
	}
	return "", false
}

// graphAPIBase is overridable in tests (request-capture fake, gate item 9's
// fallback when Meta template approval is pending).
var graphAPIBase = "https://graph.facebook.com/v20.0"

// sendSubscriberTemplate posts one Meta Business Cloud API template message
// with positional body parameters.
func sendSubscriberTemplate(ctx context.Context, client *http.Client, cfg whatsAppConfig, to, templateName, lang string, bodyParams []string) error {
	if cfg.Token == "" || cfg.PhoneID == "" {
		return fmt.Errorf("whatsapp not configured")
	}
	if to == "" {
		return fmt.Errorf("no recipient phone")
	}
	params := make([]any, 0, len(bodyParams))
	for _, p := range bodyParams {
		params = append(params, map[string]any{"type": "text", "text": p})
	}
	body, _ := json.Marshal(map[string]any{
		"messaging_product": "whatsapp",
		"to":                to,
		"type":              "template",
		"template": map[string]any{
			"name":     templateName,
			"language": map[string]any{"code": lang},
			"components": []any{map[string]any{
				"type":       "body",
				"parameters": params,
			}},
		},
	})
	url := graphAPIBase + "/" + cfg.PhoneID + "/messages"
	return postJSONAuth(ctx, client, url, body, cfg.Token)
}

// deliverSubscriberWhatsApp resolves credentials + template + language and
// sends, doing nothing (not an error) when WhatsApp isn't configured or the
// template/language isn't registered yet — delivery isolation (FR-55.4):
// missing onboarding degrades silently rather than blocking anything else.
func deliverSubscriberWhatsApp(ctx context.Context, settings platform.Settings, client *http.Client, phone, lang, templateKey string, params []string) error {
	cfg, err := platform.Get[whatsAppConfig](ctx, settings, "notifications.whatsapp")
	if err != nil || cfg.Token == "" || cfg.PhoneID == "" {
		return nil
	}
	cat, err := platform.Get[templateCatalog](ctx, settings, subscriberTemplatesKey)
	if err != nil {
		return nil
	}
	name, ok := resolveTemplate(cat, templateKey, lang)
	if !ok {
		return nil
	}
	return sendSubscriberTemplate(ctx, client, cfg, phone, name, lang, params)
}

// subscriberContact is the minimal FR-55.3 targeting data: valid phone +
// opt-in + language preference.
type subscriberContact struct {
	Name     string
	Phone    string
	OptIn    bool
	Language string
}

// loadSubscriberContact reads FR-55.3 targeting fields. ok=false on any error
// (unknown subscriber, or the migration hasn't applied yet) — callers treat
// that as "nothing to send" rather than a hard failure (NFR-7).
func loadSubscriberContact(ctx context.Context, db *pgxpool.Pool, subscriberID string) (subscriberContact, bool) {
	var c subscriberContact
	err := db.QueryRow(ctx,
		`SELECT COALESCE(name,''), COALESCE(phone,''), whatsapp_opt_in, COALESCE(language,'en')
		   FROM subscribers WHERE id = $1::uuid`,
		subscriberID).Scan(&c.Name, &c.Phone, &c.OptIn, &c.Language)
	if err != nil {
		return subscriberContact{}, false
	}
	return c, true
}
