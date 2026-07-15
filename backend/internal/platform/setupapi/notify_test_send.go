package setupapi

// POST /api/v1/settings/notifications/test ("send test Telegram/email/
// WhatsApp", FR-53 settings completion). Validates the configured credentials
// by actually attempting a send, synchronously, with a short timeout — a
// misconfigured SMTP host or bad bot token should be caught here, not three
// weeks later when the first real renewal-reminder alert silently fails
// (NFR-7: online-dependent features degrade gracefully, but "gracefully"
// still means the admin can find out before it matters).

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/smtp"
	"time"

	"github.com/hikrad/hikrad/internal/auth"
	"github.com/hikrad/hikrad/internal/httpapi"
	"github.com/hikrad/hikrad/internal/platform"
)

type smtpConfig struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	Password string `json:"password"`
	From     string `json:"from"`
}

type telegramConfig struct {
	BotToken string `json:"bot_token"`
}

type whatsappConfig struct {
	AccessToken   string `json:"access_token"`
	PhoneNumberID string `json:"phone_number_id"`
}

type testNotificationRequest struct {
	Channel   string `json:"channel" validate:"required,oneof=email telegram whatsapp"`
	Recipient string `json:"recipient" validate:"required"`
}

const testSendTimeout = 10 * time.Second

func testNotificationHandler(w http.ResponseWriter, r *http.Request) {
	var req testNotificationRequest
	if !httpapi.Bind(w, r, &req) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), testSendTimeout)
	defer cancel()

	var sendErr error
	switch req.Channel {
	case "email":
		sendErr = sendTestEmail(ctx, req.Recipient)
	case "telegram":
		sendErr = sendTestTelegram(ctx, req.Recipient)
	case "whatsapp":
		sendErr = sendTestWhatsApp(ctx, req.Recipient)
	}

	_ = auth.Audit(r.Context(), "settings.notification_test", "notifications", req.Channel, nil,
		map[string]string{"channel": req.Channel})

	if sendErr != nil {
		httpapi.JSON(w, http.StatusOK, map[string]any{"ok": false, "error": sendErr.Error()})
		return
	}
	httpapi.JSON(w, http.StatusOK, map[string]any{"ok": true})
}

func sendTestEmail(ctx context.Context, to string) error {
	cfg, err := platform.Get[smtpConfig](ctx, svc.settings, "notifications.smtp")
	if err != nil || cfg.Host == "" {
		return errors.New("SMTP is not configured (Settings > Notifications)")
	}
	from := cfg.From
	if from == "" {
		from = cfg.Username
	}
	if from == "" {
		return errors.New("SMTP 'from' address is not configured")
	}

	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("connect to %s: %w", addr, err)
	}
	defer conn.Close()

	client, err := smtp.NewClient(conn, cfg.Host)
	if err != nil {
		return fmt.Errorf("SMTP handshake: %w", err)
	}
	defer client.Close()

	if cfg.Username != "" {
		if err := client.Auth(smtp.PlainAuth("", cfg.Username, cfg.Password, cfg.Host)); err != nil {
			return fmt.Errorf("SMTP auth: %w", err)
		}
	}
	if err := client.Mail(from); err != nil {
		return err
	}
	if err := client.Rcpt(to); err != nil {
		return err
	}
	wc, err := client.Data()
	if err != nil {
		return err
	}
	msg := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: HikRAD test notification\r\n\r\nThis is a test message from your HikRAD installation.\r\n", from, to)
	if _, err := wc.Write([]byte(msg)); err != nil {
		return err
	}
	return wc.Close()
}

func sendTestTelegram(ctx context.Context, chatID string) error {
	cfg, err := platform.Get[telegramConfig](ctx, svc.settings, "notifications.telegram")
	if err != nil || cfg.BotToken == "" {
		return errors.New("Telegram bot token is not configured (Settings > Notifications)")
	}
	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", cfg.BotToken)
	body, _ := json.Marshal(map[string]string{
		"chat_id": chatID,
		"text":    "This is a test message from your HikRAD installation.",
	})
	return postJSONExpectOK(ctx, url, nil, body)
}

func sendTestWhatsApp(ctx context.Context, to string) error {
	cfg, err := platform.Get[whatsappConfig](ctx, svc.settings, "notifications.whatsapp")
	if err != nil || cfg.AccessToken == "" || cfg.PhoneNumberID == "" {
		return errors.New("WhatsApp Business Cloud API credentials are not configured (Settings > Notifications)")
	}
	url := fmt.Sprintf("https://graph.facebook.com/v19.0/%s/messages", cfg.PhoneNumberID)
	body, _ := json.Marshal(map[string]any{
		"messaging_product": "whatsapp",
		"to":                to,
		"type":              "text",
		"text":              map[string]string{"body": "This is a test message from your HikRAD installation."},
	})
	headers := map[string]string{"Authorization": "Bearer " + cfg.AccessToken}
	return postJSONExpectOK(ctx, url, headers, body)
}

func postJSONExpectOK(ctx context.Context, url string, headers map[string]string, body []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	client := &http.Client{Timeout: testSendTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("provider returned HTTP %d", resp.StatusCode)
	}
	return nil
}
