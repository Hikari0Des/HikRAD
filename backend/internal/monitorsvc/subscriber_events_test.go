package monitorsvc

// End-to-end proof of the C7 subscriber WhatsApp path (task 4b, gate item 9):
// a real Redis publish on billing.renewed flows through subscriberEvents.run
// into a request-capture fake standing in for the Meta Graph API — the same
// fallback the task brief sanctions for when Meta onboarding is pending.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/hikrad/hikrad/internal/platform"
	"github.com/hikrad/hikrad/internal/push"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

func testDBRedis(t *testing.T) (*pgxpool.Pool, *redis.Client) {
	t.Helper()
	dbURL := os.Getenv("HIKRAD_TEST_DB_URL")
	redisURL := os.Getenv("HIKRAD_TEST_REDIS_URL")
	if dbURL == "" || redisURL == "" {
		t.Skip("HIKRAD_TEST_DB_URL/HIKRAD_TEST_REDIS_URL not set; skipping monitorsvc DB+Redis suite")
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	if err := platform.Migrate(dbURL, "../../migrations", log); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	db, err := platform.NewDB(context.Background(), platform.Config{DBURL: dbURL})
	if err != nil {
		t.Fatalf("connect db: %v", err)
	}
	t.Cleanup(db.Close)
	rdb, err := platform.NewRedis(context.Background(), platform.Config{RedisURL: redisURL})
	if err != nil {
		t.Fatalf("connect redis: %v", err)
	}
	t.Cleanup(func() { _ = rdb.Close() })
	return db, rdb
}

func TestSubscriberEvents_RenewedDeliversWhatsAppReceipt(t *testing.T) {
	db, rdb := testDBRedis(t)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	captured := make(chan map[string]any, 1)
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		captured <- body
		w.WriteHeader(http.StatusOK)
	}))
	defer fake.Close()
	origBase := graphAPIBase
	graphAPIBase = fake.URL
	defer func() { graphAPIBase = origBase }()

	settings := platform.NewSettings(db)
	ctx := context.Background()
	if err := settings.Set(ctx, "notifications.whatsapp", whatsAppConfig{Token: "tok", PhoneID: "555"}); err != nil {
		t.Fatal(err)
	}
	if err := settings.Set(ctx, subscriberTemplatesKey, templateCatalog{Templates: map[string]map[string]string{
		"payment_receipt": {"ar": "payment_receipt_ar"},
	}}); err != nil {
		t.Fatal(err)
	}

	var subscriberID string
	if err := db.QueryRow(ctx,
		`INSERT INTO subscribers (username, phone, whatsapp_opt_in, language) VALUES ($1, $2, true, 'ar') RETURNING id::text`,
		fmt.Sprintf("agent2-test-%d", time.Now().UnixNano()), "9647701234567").Scan(&subscriberID); err != nil {
		t.Fatalf("seed subscriber: %v", err)
	}

	// The push channel (payment_receipt push, alongside WhatsApp) needs its own
	// wiring, same as it does in the real hikrad-monitor binary (see
	// cmd/hikrad-monitor/main.go and the push package doc comment).
	push.Init(db, rdb, settings, log)

	se := newSubscriberEvents(db, rdb, settings, log)
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go se.run(runCtx)

	// Give the subscription loop a moment to establish before publishing —
	// Redis pub/sub only delivers to already-subscribed listeners.
	time.Sleep(150 * time.Millisecond)

	payload, _ := json.Marshal(renewedEvent{
		SubscriberID: subscriberID,
		ReceiptNo:    "R-TEST-1",
		AmountIQD:    25000,
		NewExpiresAt: time.Now().Add(30 * 24 * time.Hour),
		Source:       "voucher",
	})
	if err := rdb.Publish(ctx, "billing.renewed", payload).Err(); err != nil {
		t.Fatalf("publish billing.renewed: %v", err)
	}

	select {
	case body := <-captured:
		tmpl, _ := body["template"].(map[string]any)
		if tmpl["name"] != "payment_receipt_ar" {
			t.Fatalf("template = %v, want payment_receipt_ar", tmpl["name"])
		}
		if body["to"] != "9647701234567" {
			t.Fatalf("to = %v, want the subscriber's phone", body["to"])
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the WhatsApp fake to receive the receipt request")
	}
}
