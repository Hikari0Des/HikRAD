package notify

// Channel senders (FR-160/FR-161). Five channels — in-app, Telegram, SMTP
// e-mail, WhatsApp Business Cloud, and (new this phase) SMS — behind one
// interface, all invoked ONLY by the outbox dispatcher (dispatcher.go).
// Relocated here from monitorsvc's former channels.go: this package is now
// the single place any HikRAD process turns a rendered message into a real
// network call, so admin alerts and subscriber-facing messages share one
// implementation instead of two (binding rule: no side channel).

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/smtp"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/hikrad/hikrad/internal/platform"
	"github.com/redis/go-redis/v9"
)

// NotificationsChannel is the Redis pub/sub the in-app SSE feed forwards —
// same channel name monitorsvc's live feed already subscribes to, so an
// in-app row sent from here shows up exactly where it always has.
const NotificationsChannel = "alerts:notifications"

// channelSender delivers one already-rendered row. send must be
// self-contained: honour ctx, return a real, specific error on failure (the
// TRUTHFUL reason that lands in last_error) and never panic.
type channelSender interface {
	channel() Channel
	send(ctx context.Context, row OutboxRow) error
}

func httpClient() *http.Client {
	return &http.Client{Timeout: 8 * time.Second}
}

// --- in-app -----------------------------------------------------------------

type inAppSender struct{ rdb *redis.Client }

func (inAppSender) channel() Channel { return ChannelInApp }

func (s inAppSender) send(ctx context.Context, row OutboxRow) error {
	if s.rdb == nil {
		return fmt.Errorf("redis not available")
	}
	payload, _ := json.Marshal(map[string]any{
		"type": row.Event, "summary": row.Body, "at": time.Now().UTC().Format(time.RFC3339),
	})
	return s.rdb.Publish(ctx, NotificationsChannel, payload).Err()
}

// --- telegram ---------------------------------------------------------------

type telegramConfig struct {
	BotToken string `json:"bot_token"`
	ChatID   string `json:"chat_id"`
}

type telegramSender struct {
	settings platform.Settings
	client   *http.Client
}

func (telegramSender) channel() Channel { return ChannelTelegram }

func (s telegramSender) send(ctx context.Context, row OutboxRow) error {
	cfg, err := platform.GetEncrypted[telegramConfig](ctx, s.settings, "notifications.telegram")
	if err != nil || cfg.BotToken == "" {
		return fmt.Errorf("telegram not configured")
	}
	chatID := row.Address
	if chatID == "" {
		chatID = cfg.ChatID
	}
	if chatID == "" {
		return fmt.Errorf("telegram chat_id missing")
	}
	body, _ := json.Marshal(map[string]any{"chat_id": chatID, "text": row.Body})
	url := "https://api.telegram.org/bot" + cfg.BotToken + "/sendMessage"
	return postJSON(ctx, s.client, url, body)
}

// SendTelegramText is the bot's own outbound primitive for interactive
// command replies (not a queued "message" — a synchronous response inside
// an existing conversation, the same posture as an HTTP handler's response
// body; see internal/telegrambot). It intentionally reuses the identical
// low-level call the queued channel uses so there is exactly one piece of
// code anywhere that knows how to POST to the Telegram API.
func SendTelegramText(ctx context.Context, settings platform.Settings, chatID, text string) error {
	cfg, err := platform.GetEncrypted[telegramConfig](ctx, settings, "notifications.telegram")
	if err != nil || cfg.BotToken == "" {
		return fmt.Errorf("telegram not configured")
	}
	body, _ := json.Marshal(map[string]any{"chat_id": chatID, "text": text})
	url := "https://api.telegram.org/bot" + cfg.BotToken + "/sendMessage"
	return postJSON(ctx, httpClient(), url, body)
}

// TelegramBotToken returns the configured bot token (or "" if unconfigured)
// — the bot's long-poll loop reads this once per poll to know whether to
// run at all (NFR-7: absent config, the bot simply never starts polling).
func TelegramBotToken(ctx context.Context, settings platform.Settings) string {
	cfg, err := platform.GetEncrypted[telegramConfig](ctx, settings, "notifications.telegram")
	if err != nil {
		return ""
	}
	return cfg.BotToken
}

// --- email (SMTP) -----------------------------------------------------------

type smtpConfig struct {
	Host     string   `json:"host"`
	Port     int      `json:"port"`
	Username string   `json:"username"`
	Password string   `json:"password"`
	From     string   `json:"from"`
	// To is the default staff-alert recipient list (a subscriber-targeted
	// row never reads this — its address is the subscriber's own email).
	To []string `json:"to"`
}

type emailSender struct{ settings platform.Settings }

func (emailSender) channel() Channel { return ChannelEmail }

func (s emailSender) send(ctx context.Context, row OutboxRow) error {
	cfg, err := platform.GetEncrypted[smtpConfig](ctx, s.settings, "notifications.smtp")
	if err != nil || cfg.Host == "" {
		return fmt.Errorf("smtp not configured")
	}
	if row.Address == "" {
		return fmt.Errorf("recipient email missing")
	}
	port := cfg.Port
	if port == 0 {
		port = 587
	}
	addr := cfg.Host + ":" + strconv.Itoa(port)
	subject := row.Subject
	if subject == "" {
		subject = "[HikRAD] Notification"
	}
	msg := "From: " + cfg.From + "\r\n" +
		"To: " + row.Address + "\r\n" +
		"Subject: " + subject + "\r\n\r\n" + row.Body + "\r\n"
	var auth smtp.Auth
	if cfg.Username != "" {
		auth = smtp.PlainAuth("", cfg.Username, cfg.Password, cfg.Host)
	}
	done := make(chan error, 1)
	go func() { done <- smtp.SendMail(addr, auth, cfg.From, []string{row.Address}, []byte(msg)) }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// --- whatsapp (Business Cloud API) ------------------------------------------

// whatsAppConfig's JSON tags match the panel's existing
// NotificationsSettings.tsx (access_token/phone_number_id) — the same
// "notifications.whatsapp" settings key monitorsvc used before this phase,
// unchanged, so no re-onboarding is needed.
type whatsAppConfig struct {
	Token   string `json:"access_token"`
	PhoneID string `json:"phone_number_id"`
	// To is the default staff-alert recipient list (a subscriber-targeted
	// row never reads this — its address is the subscriber's own phone).
	To []string `json:"to"`
}

// whatsAppTemplateCatalog holds the FR-55.1 approved Meta template name per
// (template key, language) — same "notifications.whatsapp_templates" key as
// before this phase.
type whatsAppTemplateCatalog struct {
	Templates map[string]map[string]string `json:"templates"`
}

func resolveWhatsAppTemplate(cat whatsAppTemplateCatalog, key, lang string) (string, bool) {
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

// graphAPIBase is overridable in tests (request-capture fake).
var graphAPIBase = "https://graph.facebook.com/v20.0"

// SetGraphAPIBaseForTest points the WhatsApp sender at a fake server for the
// duration of a test; the returned func restores the real value. Exported
// (test-only by convention/name) so other packages' integration tests
// (monitorsvc's subscriber-events suite) can exercise a real dispatcher Tick
// without talking to Meta.
func SetGraphAPIBaseForTest(url string) (restore func()) {
	orig := graphAPIBase
	graphAPIBase = url
	return func() { graphAPIBase = orig }
}

type whatsAppSender struct {
	settings platform.Settings
	client   *http.Client
}

func (whatsAppSender) channel() Channel { return ChannelWhatsApp }

// send posts a Meta template message when row.TemplateKey has an approved
// Meta template registered (FR-55.1's pre-approval requirement); otherwise
// it degrades to a plain free-text template message body is not permitted
// by Meta outside a 24h session window, so an unregistered event is a real,
// truthful failure rather than a best-effort plain send.
func (s whatsAppSender) send(ctx context.Context, row OutboxRow) error {
	cfg, err := platform.GetEncrypted[whatsAppConfig](ctx, s.settings, "notifications.whatsapp")
	if err != nil || cfg.Token == "" || cfg.PhoneID == "" {
		return fmt.Errorf("whatsapp not configured")
	}
	if row.Address == "" {
		return fmt.Errorf("recipient phone missing")
	}
	cat, err := platform.Get[whatsAppTemplateCatalog](ctx, s.settings, "notifications.whatsapp_templates")
	if err != nil {
		return fmt.Errorf("whatsapp template catalog not configured")
	}
	name, ok := resolveWhatsAppTemplate(cat, row.TemplateKey, row.Locale)
	if !ok {
		return fmt.Errorf("no approved whatsapp template for %q/%s", row.TemplateKey, row.Locale)
	}
	lang := row.Locale
	if lang == "" {
		lang = "en"
	}
	body, _ := json.Marshal(map[string]any{
		"messaging_product": "whatsapp",
		"to":                row.Address,
		"type":              "template",
		"template": map[string]any{
			"name":     name,
			"language": map[string]any{"code": lang},
			"components": []any{map[string]any{
				"type":       "body",
				"parameters": []any{map[string]any{"type": "text", "text": row.Body}},
			}},
		},
	})
	url := graphAPIBase + "/" + cfg.PhoneID + "/messages"
	return postJSONAuth(ctx, s.client, url, body, cfg.Token)
}

// --- sms (FR-161) ------------------------------------------------------------

// smsConfig is the generic HTTP gateway: a configurable URL/method/param
// mapping (any provider whose API is "POST/GET a URL with these fields")
// plus one named preset (provider != "generic" pre-fills param names so the
// operator only has to paste credentials). Secrets travel through the same
// SetEncrypted/GetEncrypted path as every other channel — no new storage.
type smsConfig struct {
	Provider string `json:"provider"` // "generic" | a named preset key
	URL      string `json:"url"`
	Method   string `json:"method"` // GET|POST
	// ParamMap: our field name -> the gateway's query/form field name, e.g.
	// {"to":"recipient","text":"message","key":"apikey"}.
	ParamMap map[string]string `json:"param_map"`
	APIKey   string            `json:"api_key"`
}

type smsSender struct {
	settings platform.Settings
	client   *http.Client
}

func (smsSender) channel() Channel { return ChannelSMS }

func (s smsSender) send(ctx context.Context, row OutboxRow) error {
	cfg, err := platform.GetEncrypted[smsConfig](ctx, s.settings, "notifications.sms")
	if err != nil || cfg.URL == "" {
		return fmt.Errorf("sms gateway not configured")
	}
	if row.Address == "" {
		return fmt.Errorf("recipient phone missing")
	}
	field := func(key, def string) string {
		if v, ok := cfg.ParamMap[key]; ok && v != "" {
			return v
		}
		return def
	}
	values := map[string]string{
		field("to", "to"):   row.Address,
		field("text", "text"): row.Body,
	}
	if cfg.APIKey != "" {
		values[field("key", "key")] = cfg.APIKey
	}
	method := strings.ToUpper(cfg.Method)
	if method == "" {
		method = http.MethodPost
	}
	return sendSMSHTTP(ctx, s.client, method, cfg.URL, values)
}

func sendSMSHTTP(ctx context.Context, client *http.Client, method, rawURL string, values map[string]string) error {
	if client == nil {
		client = httpClient()
	}
	form := make([]string, 0, len(values))
	for k, v := range values {
		form = append(form, k+"="+url.QueryEscape(v))
	}
	qs := strings.Join(form, "&")
	var req *http.Request
	var err error
	if method == http.MethodGet {
		sep := "?"
		if strings.Contains(rawURL, "?") {
			sep = "&"
		}
		req, err = http.NewRequestWithContext(ctx, http.MethodGet, rawURL+sep+qs, nil)
	} else {
		req, err = http.NewRequestWithContext(ctx, http.MethodPost, rawURL, strings.NewReader(qs))
		if err == nil {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
	}
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("http %d", resp.StatusCode)
	}
	return nil
}

// --- http helpers -----------------------------------------------------------

func postJSON(ctx context.Context, client *http.Client, url string, body []byte) error {
	return postJSONAuth(ctx, client, url, body, "")
}

func postJSONAuth(ctx context.Context, client *http.Client, url string, body []byte, bearer string) error {
	if client == nil {
		client = httpClient()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("http %d", resp.StatusCode)
	}
	return nil
}

