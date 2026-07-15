package monitorsvc

// End-to-end proof of the per-subscriber expiring-reminder targeting (task
// 4/4b, contract C4/C7): a subscriber crossing the threshold gets a WhatsApp
// expiry_reminder, proven against a request-capture fake.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hikrad/hikrad/internal/platform"
	"github.com/hikrad/hikrad/internal/push"
)

func TestDigestPerSubscriber_ExpiryReminderWhatsApp(t *testing.T) {
	db, rdb := testDBRedis(t)
	ctx := context.Background()
	settings := platform.NewSettings(db)
	push.Init(db, rdb, settings, slog.New(slog.NewTextHandler(io.Discard, nil)))

	captured := make(chan map[string]any, 1)
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		select {
		case captured <- body:
		default:
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer fake.Close()
	origBase := graphAPIBase
	graphAPIBase = fake.URL
	defer func() { graphAPIBase = origBase }()

	if err := settings.Set(ctx, "notifications.whatsapp", whatsAppConfig{Token: "tok", PhoneID: "555"}); err != nil {
		t.Fatal(err)
	}
	if err := settings.Set(ctx, subscriberTemplatesKey, templateCatalog{Templates: map[string]map[string]string{
		"expiry_reminder": {"ar": "expiry_reminder_ar"},
	}}); err != nil {
		t.Fatal(err)
	}

	// A subscriber expiring in 2 days, inside a 3-day digest window.
	if _, err := db.Exec(ctx,
		`INSERT INTO subscribers (username, phone, whatsapp_opt_in, language, status, expires_at)
		 VALUES ($1, $2, true, 'ar', 'active', now() + interval '2 days')`,
		fmt.Sprintf("agent2-digest-%d", time.Now().UnixNano()), "9647709999999"); err != nil {
		t.Fatalf("seed subscriber: %v", err)
	}
	// A control subscriber well outside the window must NOT trigger a send.
	if _, err := db.Exec(ctx,
		`INSERT INTO subscribers (username, phone, whatsapp_opt_in, language, status, expires_at)
		 VALUES ($1, $2, true, 'ar', 'active', now() + interval '90 days')`,
		fmt.Sprintf("agent2-digest-control-%d", time.Now().UnixNano()), "9647708888888"); err != nil {
		t.Fatalf("seed control subscriber: %v", err)
	}

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cond := newConditions(db, rdb, settings, newAlertEngine(db, rdb, settings, log), log)
	cond.digestPerSubscriber(ctx, 3)

	select {
	case body := <-captured:
		tmpl, _ := body["template"].(map[string]any)
		if tmpl["name"] != "expiry_reminder_ar" {
			t.Fatalf("template = %v, want expiry_reminder_ar", tmpl["name"])
		}
		if body["to"] != "9647709999999" {
			t.Fatalf("to = %v, want the expiring subscriber's phone (not the control)", body["to"])
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for the expiry reminder WhatsApp send")
	}
}
