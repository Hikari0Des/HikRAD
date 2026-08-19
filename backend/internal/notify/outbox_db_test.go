package notify

// Gate items 1 (one queue), 2 (outbox truthful), 3 (SMS encrypted at rest),
// 4 (opt-out honoured / ignore_opt_out), 13 (migrations 0625-0630 apply
// cleanly — implicit: every test here runs platform.Migrate first). Package
// notify (not notify_test) so these can call insertOutbox/EnqueueEvent/etc.
// directly, same convention internal/radius's coa_outbox_db_test.go uses.

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/hikrad/hikrad/internal/platform"
	"github.com/jackc/pgx/v5/pgxpool"
)

func testDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dbURL := os.Getenv("HIKRAD_TEST_DB_URL")
	if dbURL == "" {
		t.Skip("HIKRAD_TEST_DB_URL not set; skipping notify DB suite")
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
	return db
}

var suffixCounter int64

func randSuffix() string {
	suffixCounter++
	return time.Now().Format("150405.000000") + "-" + string(rune('a'+suffixCounter%26))
}

func seedSubscriber(t *testing.T, db *pgxpool.Pool, phone string, whatsappOptIn bool) string {
	t.Helper()
	var id string
	err := db.QueryRow(context.Background(),
		`INSERT INTO subscribers (username, phone, whatsapp_opt_in, status)
		 VALUES ($1, $2, $3, 'active') RETURNING id::text`,
		"notify-test-"+randSuffix(), phone, whatsappOptIn).Scan(&id)
	if err != nil {
		t.Fatalf("seed subscriber: %v", err)
	}
	return id
}

// emptySettings is a platform.Settings with nothing ever configured — used
// to exercise "channel not configured" without touching the crypto service.
type emptySettings struct{}

func (emptySettings) GetRaw(context.Context, string) (json.RawMessage, error) {
	return nil, platform.ErrSettingNotFound
}
func (emptySettings) Set(context.Context, string, any) error { return nil }
func (emptySettings) Invalidate(string)                      {}
func (emptySettings) OnChange(func(string))                  {}

// --- gate item 1: one queue ------------------------------------------------

func TestOneQueue_EveryEnqueuePathAppearsInOutbox(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	sub := seedSubscriber(t, db, "9647700000001", false)

	before, err := outboxCounters(ctx, db)
	if err != nil {
		t.Fatal(err)
	}

	// Transactional (rule-driven; payment_receipt ships ignore_opt_out=true).
	if _, err := EnqueueEvent(ctx, db, EnqueueInput{EventKey: EventPaymentReceipt, SubscriberID: sub}); err != nil {
		t.Fatalf("EnqueueEvent: %v", err)
	}
	// Staff alert.
	if err := EnqueueAlert(ctx, db, emptySettings{}, AlertEnqueueInput{RuleType: "nas_down", Summary: "test", Channels: []string{"inapp"}}); err != nil {
		t.Fatalf("EnqueueAlert: %v", err)
	}
	// Broadcast.
	if _, err := EnqueueBroadcast(ctx, db, BroadcastInput{SubscriberIDs: []string{sub}, Channel: ChannelInApp, Body: "hello"}); err != nil {
		t.Fatalf("EnqueueBroadcast: %v", err)
	}

	after, err := outboxCounters(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if after.Total-before.Total < 3 {
		t.Fatalf("expected at least 3 new rows in the ONE outbox across all three enqueue paths, got %d", after.Total-before.Total)
	}

	var sources []string
	rows, err := db.Query(ctx, `SELECT DISTINCT source FROM notification_outbox WHERE source IN ('rule','alert','broadcast')`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatal(err)
		}
		sources = append(sources, s)
	}
	for _, want := range []string{"rule", "alert", "broadcast"} {
		found := false
		for _, s := range sources {
			if s == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("source %q never appeared in notification_outbox — that path used a side channel instead of the queue", want)
		}
	}
}

// --- gate item 2: outbox truthful ------------------------------------------

func TestDispatcher_TruthfulFailureReason(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	id, err := insertOutbox(ctx, db, OutboxInsert{
		Source: "broadcast", Event: "broadcast", Address: "9647700000002",
		Channel: ChannelSMS, Locale: "en", Body: "test message",
	})
	if err != nil {
		t.Fatal(err)
	}

	d := NewDispatcher(db, nil, emptySettings{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	d.Tick(ctx, 10)

	row, err := getOutbox(ctx, db, id)
	if err != nil {
		t.Fatal(err)
	}
	if row.State != string(StateQueued) && row.State != string(StateFailed) {
		t.Fatalf("state = %q, want queued (retry pending) or failed", row.State)
	}
	if row.LastError == "" {
		t.Fatal("last_error is empty — the outbox must show a real reason, never silence")
	}
	if row.LastError == "failed" || row.LastError == "error" {
		t.Fatalf("last_error = %q — a generic placeholder, not a truthful reason", row.LastError)
	}
	if !strings.Contains(row.LastError, "sms gateway not configured") {
		t.Fatalf("last_error = %q, want it to name the real cause (sms gateway not configured)", row.LastError)
	}
}

// --- gate item 3: SMS channel secret encrypted at rest ---------------------

func TestSMSChannelSecret_EncryptedAtRest(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	settings := platform.NewSettings(db)

	plainKey := "super-secret-api-key-should-never-appear-in-plaintext"
	if err := platform.SetEncrypted(ctx, settings, "notifications.sms", smsConfig{
		Provider: "generic", URL: "https://sms.example.test/send", Method: "POST", APIKey: plainKey,
	}); err != nil {
		t.Fatalf("SetEncrypted: %v", err)
	}

	// Inspect the actual stored value (item 3: "verified in the DB", not
	// assumed) — must not contain the plaintext secret anywhere, and must
	// decode as base64 ciphertext, not as the plain smsConfig JSON shape.
	var storedRaw []byte
	if err := db.QueryRow(ctx, `SELECT value FROM settings WHERE key = 'notifications.sms'`).Scan(&storedRaw); err != nil {
		t.Fatalf("read raw settings row: %v", err)
	}
	if strings.Contains(string(storedRaw), plainKey) {
		t.Fatalf("plaintext API key found in the settings table: %s", storedRaw)
	}
	if strings.Contains(string(storedRaw), "api_key") {
		t.Fatalf("stored value still looks like plain JSON (contains the field name \"api_key\"): %s", storedRaw)
	}
	var b64 string
	if err := json.Unmarshal(storedRaw, &b64); err != nil {
		t.Fatalf("stored value is not a JSON string (expected base64 ciphertext): %v", err)
	}
	if _, err := base64.StdEncoding.DecodeString(b64); err != nil {
		t.Fatalf("stored value is not valid base64 ciphertext: %v", err)
	}

	// Round-trips back to the original plaintext through the encryption service.
	cfg, err := platform.GetEncrypted[smsConfig](ctx, settings, "notifications.sms")
	if err != nil {
		t.Fatalf("GetEncrypted: %v", err)
	}
	if cfg.APIKey != plainKey {
		t.Fatalf("round-tripped api key = %q, want %q", cfg.APIKey, plainKey)
	}
}

// --- gate item 4: opt-out honoured / ignore_opt_out override ---------------

func TestOptOut_HonouredUnlessRuleIgnoresIt(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	sub := seedSubscriber(t, db, "9647700000003", false)

	if _, err := db.Exec(ctx, `INSERT INTO subscriber_channel_opt_ins (subscriber_id, channel, opted_in) VALUES ($1::uuid, 'sms', false)`, sub); err != nil {
		t.Fatal(err)
	}

	// renewal_reminder ships with ignore_opt_out=false (migration 0626).
	if _, _, err := updateRule(ctx, db, EventRenewalReminder, RuleUpdate{Channels: []string{"sms"}}); err != nil {
		t.Fatal(err)
	}
	ids, err := EnqueueEvent(ctx, db, EnqueueInput{EventKey: EventRenewalReminder, SubscriberID: sub, Params: map[string]string{"days_left": "3"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 {
		t.Fatalf("expected 1 row (sms), got %d", len(ids))
	}
	row, err := getOutbox(ctx, db, ids[0])
	if err != nil {
		t.Fatal(err)
	}
	if row.State != string(StateSkipped) || row.SkipReason != SkipOptedOut {
		t.Fatalf("opted-out subscriber: state=%q skip_reason=%q, want skipped/opted_out", row.State, row.SkipReason)
	}

	// Flip the rule to ignore_opt_out=true: the same subscriber now gets queued.
	ignoreTrue := true
	if _, _, err := updateRule(ctx, db, EventRenewalReminder, RuleUpdate{IgnoreOptOut: &ignoreTrue}); err != nil {
		t.Fatal(err)
	}
	ids2, err := EnqueueEvent(ctx, db, EnqueueInput{EventKey: EventRenewalReminder, SubscriberID: sub, Params: map[string]string{"days_left": "3"}})
	if err != nil {
		t.Fatal(err)
	}
	row2, err := getOutbox(ctx, db, ids2[0])
	if err != nil {
		t.Fatal(err)
	}
	if row2.State == string(StateSkipped) && row2.SkipReason == SkipOptedOut {
		t.Fatal("ignore_opt_out=true must still deliver to an opted-out subscriber, but it was skipped as opted_out")
	}

	// Reset the rule back to its shipped default so other tests in this
	// package (which also touch renewal_reminder) aren't affected by
	// ordering.
	ignoreFalse := false
	if _, _, err := updateRule(ctx, db, EventRenewalReminder, RuleUpdate{IgnoreOptOut: &ignoreFalse, Channels: []string{"whatsapp", "sms", "inapp"}}); err != nil {
		t.Fatal(err)
	}
}

// --- gate item 7: throttling ------------------------------------------------

// TestThrottling_BatchSizeRespectedUnderLargeBroadcast is the literal gate
// scenario: a 1,000-message broadcast must not all land in `sending`/`sent`
// in a single dispatcher tick — the batch size caps how many rows one tick
// claims, exactly like coa_dispatcher's own claimBatch(limit).
func TestThrottling_BatchSizeRespectedUnderLargeBroadcast(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	const total = 1000
	const batchSize = 50
	marker := "throttle test " + randSuffix()
	ids := make([]string, total)
	for i := 0; i < total; i++ {
		ids[i] = seedSubscriber(t, db, "", false) // no phone -> inapp only, always deliverable
	}
	if _, err := EnqueueBroadcast(ctx, db, BroadcastInput{SubscriberIDs: ids, Channel: ChannelInApp, Body: marker}); err != nil {
		t.Fatal(err)
	}

	countByState := func(state string) int64 {
		var n int64
		if err := db.QueryRow(ctx,
			`SELECT count(*) FROM notification_outbox WHERE body = $1 AND state = $2`, marker, state).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	if got := countByState(string(StateQueued)); got != total {
		t.Fatalf("queued immediately after enqueue = %d, want %d", got, total)
	}

	d := NewDispatcher(db, nil, emptySettings{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	d.Tick(ctx, batchSize)

	settled := countByState(string(StateSent)) + countByState(string(StateFailed)) + countByState(string(StateSkipped))
	stillQueued := countByState(string(StateQueued))
	if settled > batchSize {
		t.Fatalf("one tick settled %d of this broadcast's own rows, want at most the batch size %d — throttling not respected", settled, batchSize)
	}
	if stillQueued != int64(total)-settled {
		t.Fatalf("queued=%d settled=%d don't sum to the broadcast's %d rows", stillQueued, settled, total)
	}
	if stillQueued == 0 {
		t.Fatalf("all %d rows settled in a single tick — a 1,000-message broadcast must span multiple ticks under a %d batch size", total, batchSize)
	}
}

// --- gate item 5: templates trilingual --------------------------------------

// TestDefaultTemplates_AllEventsTrilingual is the concrete DB-level proof
// behind gate item 5 ("every default template exists in en/ar/ku"): every
// event this phase ships a rule for (migration 0626's seed) must have a
// current template in all three locales, with no empty body.
func TestDefaultTemplates_AllEventsTrilingual(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	events := []string{
		EventRenewalReminder, EventExpiryNotice, EventQuotaWarning,
		EventPaymentReceipt, EventPlanRenewed, EventPaymentTicketState, EventStaffAlert,
	}
	for _, ev := range events {
		for _, locale := range []string{"en", "ar", "ku"} {
			tpl, err := currentTemplate(ctx, db, ev, locale)
			if err != nil {
				t.Fatalf("event %q locale %q: no template found: %v", ev, locale, err)
			}
			if tpl.Locale != locale {
				t.Fatalf("event %q locale %q: fell back to %q instead of having its own row — not shipped trilingual", ev, locale, tpl.Locale)
			}
			if tpl.Body == "" {
				t.Fatalf("event %q locale %q: template body is empty", ev, locale)
			}
		}
	}
}
