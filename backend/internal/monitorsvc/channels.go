package monitorsvc

// Alert delivery channels (FR-36, Decision 16). Four channels — in-app, Telegram,
// SMTP e-mail, WhatsApp Business Cloud — behind one interface. The dispatcher
// runs them concurrently so a dead endpoint (offline ISP, un-onboarded WhatsApp)
// is logged and retried WITHOUT delaying the others: in-app and Telegram are
// primary per the NFR-7 posture, and a stuck SMTP dial must never hold up the
// live notification. The WhatsApp sender is built reusable for the FR-55
// subscriber-facing path that lands in Phase 4.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/smtp"
	"strconv"
	"strings"
	"time"

	"github.com/hikrad/hikrad/internal/platform"
	"github.com/redis/go-redis/v9"
)

// Frozen channel names (contract C5 enum).
const (
	chInApp    = "inapp"
	chTelegram = "telegram"
	chEmail    = "email"
	chWhatsApp = "whatsapp"
	chPush     = "push" // Phase 4, contract C4/FR-54.4
)

// NotificationsChannel is the Redis pub/sub the in-app SSE feed forwards.
const NotificationsChannel = "alerts:notifications"

// alertMessage is one rule fire rendered for delivery.
type alertMessage struct {
	RuleType   string
	State      string // firing | resolved
	Summary    string
	Payload    map[string]any
	Recipients map[string]json.RawMessage // per-rule channel overrides (C5)
}

// text is the human line a channel sends.
func (m alertMessage) text() string {
	prefix := "🔴"
	if m.State == "resolved" {
		prefix = "🟢"
	}
	return prefix + " " + m.Summary
}

// channelSender delivers to one channel. send must be self-contained: honour ctx,
// return an error on failure (the dispatcher logs + retries), never panic.
type channelSender interface {
	channel() string
	send(ctx context.Context, m alertMessage) error
}

// delivery is the per-channel outcome recorded on the alert_events row.
type delivery struct {
	Channel string `json:"channel"`
	OK      bool   `json:"ok"`
	Detail  string `json:"detail,omitempty"`
}

// dispatcher fans a message out to the requested channels concurrently with a
// bounded retry per channel, returning one delivery result each.
type dispatcher struct {
	senders map[string]channelSender
	log     *slog.Logger
	retries int
}

func newDispatcher(log *slog.Logger, senders ...channelSender) *dispatcher {
	m := make(map[string]channelSender, len(senders))
	for _, s := range senders {
		m[s.channel()] = s
	}
	return &dispatcher{senders: m, log: log, retries: 2}
}

// dispatch sends m to each named channel independently. Channels run in parallel
// so a slow/failing one cannot delay another (failure isolation, gate item 6).
func (d *dispatcher) dispatch(ctx context.Context, channels []string, m alertMessage) []delivery {
	type res struct {
		i int
		d delivery
	}
	out := make([]delivery, len(channels))
	ch := make(chan res, len(channels))
	for i, name := range channels {
		s := d.senders[name]
		if s == nil {
			out[i] = delivery{Channel: name, OK: false, Detail: "unknown channel"}
			ch <- res{i, out[i]}
			continue
		}
		go func(i int, s channelSender) {
			ch <- res{i, d.sendWithRetry(ctx, s, m)}
		}(i, s)
	}
	for range channels {
		r := <-ch
		out[r.i] = r.d
	}
	return out
}

func (d *dispatcher) sendWithRetry(ctx context.Context, s channelSender, m alertMessage) delivery {
	var lastErr error
	for attempt := 0; attempt <= d.retries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return delivery{Channel: s.channel(), OK: false, Detail: "context cancelled"}
			case <-time.After(time.Duration(attempt) * 300 * time.Millisecond):
			}
		}
		if err := s.send(ctx, m); err == nil {
			return delivery{Channel: s.channel(), OK: true}
		} else {
			lastErr = err
		}
	}
	if d.log != nil {
		d.log.Warn("alert channel delivery failed", "channel", s.channel(), "error", lastErr)
	}
	return delivery{Channel: s.channel(), OK: false, Detail: lastErr.Error()}
}

// --- in-app -----------------------------------------------------------------

// inAppSender publishes the notification for the SSE feed; persistence is the
// alert_events row the caller writes. Never fails on a nil Redis (degraded).
type inAppSender struct{ rdb *redis.Client }

func (inAppSender) channel() string { return chInApp }

func (s inAppSender) send(ctx context.Context, m alertMessage) error {
	if s.rdb == nil {
		return nil
	}
	payload, _ := json.Marshal(map[string]any{
		"type": m.RuleType, "state": m.State, "summary": m.Summary, "at": time.Now().UTC().Format(time.RFC3339),
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

func (telegramSender) channel() string { return chTelegram }

func (s telegramSender) send(ctx context.Context, m alertMessage) error {
	cfg, err := platform.Get[telegramConfig](ctx, s.settings, "notifications.telegram")
	if err != nil || cfg.BotToken == "" {
		return fmt.Errorf("telegram not configured")
	}
	chatID := cfg.ChatID
	if raw, ok := m.Recipients["telegram_chat_id"]; ok {
		var c string
		if json.Unmarshal(raw, &c) == nil && c != "" {
			chatID = c
		}
	}
	if chatID == "" {
		return fmt.Errorf("telegram chat_id missing")
	}
	body, _ := json.Marshal(map[string]any{"chat_id": chatID, "text": m.text()})
	url := "https://api.telegram.org/bot" + cfg.BotToken + "/sendMessage"
	return postJSON(ctx, s.client, url, body)
}

// --- email (SMTP) -----------------------------------------------------------

type smtpConfig struct {
	Host     string   `json:"host"`
	Port     int      `json:"port"`
	Username string   `json:"username"`
	Password string   `json:"password"`
	From     string   `json:"from"`
	To       []string `json:"to"`
}

type emailSender struct{ settings platform.Settings }

func (emailSender) channel() string { return chEmail }

func (s emailSender) send(ctx context.Context, m alertMessage) error {
	cfg, err := platform.Get[smtpConfig](ctx, s.settings, "notifications.smtp")
	if err != nil || cfg.Host == "" {
		return fmt.Errorf("smtp not configured")
	}
	to := cfg.To
	if raw, ok := m.Recipients["email_to"]; ok {
		var override []string
		if json.Unmarshal(raw, &override) == nil && len(override) > 0 {
			to = override
		}
	}
	if len(to) == 0 {
		return fmt.Errorf("smtp recipients missing")
	}
	port := cfg.Port
	if port == 0 {
		port = 587
	}
	addr := cfg.Host + ":" + strconv.Itoa(port)
	msg := "From: " + cfg.From + "\r\n" +
		"To: " + strings.Join(to, ", ") + "\r\n" +
		"Subject: [HikRAD] " + m.Summary + "\r\n\r\n" + m.text() + "\r\n"
	var auth smtp.Auth
	if cfg.Username != "" {
		auth = smtp.PlainAuth("", cfg.Username, cfg.Password, cfg.Host)
	}
	// smtp.SendMail has no ctx; run it in a goroutine bounded by ctx so a stuck
	// dial cannot outlive the dispatch (failure isolation).
	done := make(chan error, 1)
	go func() { done <- smtp.SendMail(addr, auth, cfg.From, to, []byte(msg)) }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// --- whatsapp (Business Cloud API) ------------------------------------------

type whatsAppConfig struct {
	Token    string   `json:"token"`
	PhoneID  string   `json:"phone_id"`
	Template string   `json:"template"` // approved alert template name
	Lang     string   `json:"lang"`
	To       []string `json:"to"`
}

// whatsAppSender posts a template message. Reusable for FR-55 (Phase 4) — the
// subscriber-facing path supplies its own template + recipient.
type whatsAppSender struct {
	settings platform.Settings
	client   *http.Client
}

func (whatsAppSender) channel() string { return chWhatsApp }

func (s whatsAppSender) send(ctx context.Context, m alertMessage) error {
	cfg, err := platform.Get[whatsAppConfig](ctx, s.settings, "notifications.whatsapp")
	if err != nil || cfg.Token == "" || cfg.PhoneID == "" {
		return fmt.Errorf("whatsapp not configured")
	}
	to := cfg.To
	if raw, ok := m.Recipients["whatsapp_to"]; ok {
		var override []string
		if json.Unmarshal(raw, &override) == nil && len(override) > 0 {
			to = override
		}
	}
	if len(to) == 0 {
		return fmt.Errorf("whatsapp recipients missing")
	}
	lang := cfg.Lang
	if lang == "" {
		lang = "en"
	}
	url := graphAPIBase + "/" + cfg.PhoneID + "/messages"
	var firstErr error
	for _, number := range to {
		// Generic approved alert template with one text body param (the summary).
		body, _ := json.Marshal(map[string]any{
			"messaging_product": "whatsapp",
			"to":                number,
			"type":              "template",
			"template": map[string]any{
				"name":     cfg.Template,
				"language": map[string]any{"code": lang},
				"components": []any{map[string]any{
					"type":       "body",
					"parameters": []any{map[string]any{"type": "text", "text": m.text()}},
				}},
			},
		})
		if err := postJSONAuth(ctx, s.client, url, body, cfg.Token); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// --- http helpers -----------------------------------------------------------

func postJSON(ctx context.Context, client *http.Client, url string, body []byte) error {
	return postJSONAuth(ctx, client, url, body, "")
}

func postJSONAuth(ctx context.Context, client *http.Client, url string, body []byte, bearer string) error {
	if client == nil {
		client = &http.Client{Timeout: 8 * time.Second}
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
