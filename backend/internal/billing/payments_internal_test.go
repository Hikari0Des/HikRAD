package billing

// In-package DB tests for the pieces that need unexported access: the
// callback confirm/idempotency core, the reconciliation worker's timing
// logic, and the scratch-card trial/approve/reject math (Phase 4, C3/C8).
// Gated on HIKRAD_TEST_DB_URL, same convention as unit_test.go/db_test.go.

import (
	"context"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/hikrad/hikrad/internal/billing/gateways"
	"github.com/hikrad/hikrad/internal/platform"
	"github.com/hikrad/hikrad/internal/platform/crypto"
	"github.com/jackc/pgx/v5/pgxpool"
)

func internalDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dbURL := os.Getenv("HIKRAD_TEST_DB_URL")
	if dbURL == "" {
		t.Skip("HIKRAD_TEST_DB_URL not set; skipping billing internal DB tests")
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	if err := platform.Migrate(dbURL, "../../migrations", log); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	db, err := platform.NewDB(context.Background(), platform.Config{DBURL: dbURL})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(db.Close)

	// crypto is normally configured once at boot (auth.Module.Register); this
	// suite never boots the router, so install a deterministic test key
	// directly (submitCard/revealCard need it for card_code_enc).
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 11)
	}
	if err := crypto.Configure(key); err != nil {
		t.Fatalf("configure crypto: %v", err)
	}
	return db
}

func testModule(db *pgxpool.Pool) *Module {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return &Module{db: db, log: log}
}

func mkTestProfile(t *testing.T, db *pgxpool.Pool, price int64, days int) string {
	t.Helper()
	var id string
	if err := db.QueryRow(context.Background(),
		`INSERT INTO profiles (name, price_iqd, duration_days, rate_down_kbps, rate_up_kbps)
		 VALUES ($1, $2, $3, 10240, 2048) RETURNING id::text`,
		uniqTestName("prof"), price, days).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func mkTestSubscriber(t *testing.T, db *pgxpool.Pool, profileID string, expiresAt *time.Time) string {
	t.Helper()
	var id string
	if err := db.QueryRow(context.Background(),
		`INSERT INTO subscribers (username, password_enc, status, profile_id, expires_at)
		 VALUES ($1, '\x0102'::bytea, 'active', $2::uuid, $3) RETURNING id::text`,
		uniqTestName("sub"), profileID, expiresAt).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func uniqTestName(p string) string {
	return p + "-" + time.Now().Format("150405.000000000")
}

// --- Payment intent callback core -------------------------------------------

func TestProcessCallbackAmountMismatch(t *testing.T) {
	db := internalDB(t)
	m := testModule(db)
	ctx := context.Background()
	prof := mkTestProfile(t, db, 25000, 30)
	sub := mkTestSubscriber(t, db, prof, nil)

	ref := uniqTestName("ref")
	var intentID string
	if err := db.QueryRow(ctx,
		`INSERT INTO payment_intents (subscriber_id, profile_id, gateway, amount_iqd, gateway_ref)
		 VALUES ($1::uuid, $2::uuid, 'mock', 25000, $3) RETURNING id::text`, sub, prof, ref).Scan(&intentID); err != nil {
		t.Fatal(err)
	}

	err := m.processCallback(ctx, "mock", gateways.CallbackResult{
		OrderID: intentID, GatewayRef: ref, State: gateways.StateConfirmed, AmountIQD: 9999,
	})
	if err != errAmountMismatch {
		t.Fatalf("expected errAmountMismatch, got %v", err)
	}
	var state string
	_ = db.QueryRow(ctx, `SELECT state FROM payment_intents WHERE id = $1::uuid`, intentID).Scan(&state)
	if state != "pending" {
		t.Fatalf("intent state = %q, want pending (rejected callback must not confirm)", state)
	}
}

// TestProcessCallbackReplayIdempotent — three sequential "confirmed" callbacks
// for the same intent (simulating replay) must produce exactly one renewal
// (contract C3, AC-23a).
func TestProcessCallbackReplayIdempotent(t *testing.T) {
	db := internalDB(t)
	m := testModule(db)
	ctx := context.Background()
	prof := mkTestProfile(t, db, 25000, 30)
	sub := mkTestSubscriber(t, db, prof, nil)

	ref := uniqTestName("ref")
	var intentID string
	if err := db.QueryRow(ctx,
		`INSERT INTO payment_intents (subscriber_id, profile_id, gateway, amount_iqd, gateway_ref)
		 VALUES ($1::uuid, $2::uuid, 'mock', 25000, $3) RETURNING id::text`, sub, prof, ref).Scan(&intentID); err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 3; i++ {
		if err := m.processCallback(ctx, "mock", gateways.CallbackResult{
			OrderID: intentID, GatewayRef: ref, State: gateways.StateConfirmed, AmountIQD: 25000,
		}); err != nil {
			t.Fatalf("replay %d: %v", i, err)
		}
	}

	var n int
	_ = db.QueryRow(ctx,
		`SELECT count(*) FROM ledger_transactions WHERE subscriber_id = $1::uuid AND source = 'portal-mock'`, sub).Scan(&n)
	if n != 1 {
		t.Fatalf("expected exactly 1 renewal ledger row after 3 replays, got %d", n)
	}
	var state string
	_ = db.QueryRow(ctx, `SELECT state FROM payment_intents WHERE id = $1::uuid`, intentID).Scan(&state)
	if state != "renewed" {
		t.Fatalf("intent state = %q, want renewed", state)
	}
}

// --- Reconciliation worker ---------------------------------------------------

func TestReconcileExpiresStaleIntent(t *testing.T) {
	db := internalDB(t)
	m := testModule(db)
	ctx := context.Background()
	prof := mkTestProfile(t, db, 1000, 30)
	sub := mkTestSubscriber(t, db, prof, nil)

	var intentID string
	if err := db.QueryRow(ctx,
		`INSERT INTO payment_intents (subscriber_id, profile_id, gateway, amount_iqd, gateway_ref, created_at)
		 VALUES ($1::uuid, $2::uuid, 'mock', 1000, $3, now() - interval '49 hours') RETURNING id::text`,
		sub, prof, uniqTestName("ref")).Scan(&intentID); err != nil {
		t.Fatal(err)
	}

	m.reconcileOnce(ctx)

	var state string
	_ = db.QueryRow(ctx, `SELECT state FROM payment_intents WHERE id = $1::uuid`, intentID).Scan(&state)
	if state != "expired" {
		t.Fatalf("intent state = %q, want expired (age > 48h)", state)
	}
}

// --- Scratch-card trial/approve/reject math (FR-59, AC-59a/b/c) -------------

func TestCardTrialGuardsAndApproveAnchoring(t *testing.T) {
	db := internalDB(t)
	m := testModule(db)
	ctx := context.Background()
	prof := mkTestProfile(t, db, 25000, 30)
	past := time.Now().UTC().Add(-24 * time.Hour) // already expired
	sub := mkTestSubscriber(t, db, prof, &past)

	res, err := m.submitCard(ctx, sub, "zain", "1234-5678-9012")
	if err != nil {
		t.Fatalf("submitCard: %v", err)
	}
	if res.State != "pending" {
		t.Fatalf("state = %q, want pending", res.State)
	}

	// AC-59a: 1-day trial applied immediately.
	var expiresAt time.Time
	var status string
	if err := db.QueryRow(ctx, `SELECT expires_at, status FROM subscribers WHERE id = $1::uuid`, sub).
		Scan(&expiresAt, &status); err != nil {
		t.Fatal(err)
	}
	if status != "active" {
		t.Fatalf("status after trial = %q, want active", status)
	}
	wantExpiry := time.Now().UTC().AddDate(0, 0, 1)
	if diff := expiresAt.Sub(wantExpiry); diff < -time.Minute || diff > time.Minute {
		t.Fatalf("trial expiry = %v, want ~%v", expiresAt, wantExpiry)
	}

	// FR-59.4: one pending per subscriber — a second submission is rejected.
	if _, err := m.submitCard(ctx, sub, "zain", "9999-9999-9999"); err != errCardPending {
		t.Fatalf("second submit while pending: got %v, want errCardPending", err)
	}

	// AC-59c: reveal returns the code; list never carries it.
	var cardID string
	if err := db.QueryRow(ctx, `SELECT id::text FROM card_payments WHERE subscriber_id = $1::uuid`, sub).Scan(&cardID); err != nil {
		t.Fatal(err)
	}
	code, err := m.revealCard(ctx, cardID)
	if err != nil || code != "1234-5678-9012" {
		t.Fatalf("revealCard = %q, %v, want the submitted code", code, err)
	}
	// card_payments isn't a per-test table, so a prior run against this same
	// persistent DB may have left other pending rows queued (confirmed while
	// hardening scripts/gate-phase-4.sh for repeat runs) — find this test's
	// own row rather than asserting the pending queue is a singleton.
	items, err := m.listCardPayments(ctx, "pending")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, it := range items {
		if it.ID == cardID {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("listCardPayments = %+v, want an entry for %s", items, cardID)
	}

	// AC-59b: approve anchors at the trial's start — 30-day profile, trial
	// included not added, so new expiry = trial_start + 30 days (not +31).
	var trialStart time.Time
	if err := db.QueryRow(ctx, `SELECT trial_started_at FROM card_payments WHERE id = $1::uuid`, cardID).Scan(&trialStart); err != nil {
		t.Fatal(err)
	}
	rr, err := m.approveCard(ctx, cardID, "")
	if err != nil {
		t.Fatalf("approveCard: %v", err)
	}
	wantApproved := trialStart.UTC().AddDate(0, 0, 30)
	if diff := rr.NewExpiresAt.Sub(wantApproved); diff < -time.Second || diff > time.Second {
		t.Fatalf("approved expiry = %v, want %v (trial start + 30d)", rr.NewExpiresAt, wantApproved)
	}

	// A second approve of the now-decided card is rejected.
	if _, err := m.approveCard(ctx, cardID, ""); err != errCardNotPending {
		t.Fatalf("second approve: got %v, want errCardNotPending", err)
	}
}

// TestCardRejectNetsZeroAndCooldown covers AC-59b's reject half + FR-59.4's
// post-rejection cooldown guard.
func TestCardRejectNetsZeroAndCooldown(t *testing.T) {
	db := internalDB(t)
	m := testModule(db)
	ctx := context.Background()
	prof := mkTestProfile(t, db, 25000, 30)
	past := time.Now().UTC().Add(-24 * time.Hour)
	sub := mkTestSubscriber(t, db, prof, &past)

	if _, err := m.submitCard(ctx, sub, "asiacell", "0000-1111-2222"); err != nil {
		t.Fatalf("submitCard: %v", err)
	}
	var cardID string
	if err := db.QueryRow(ctx, `SELECT id::text FROM card_payments WHERE subscriber_id = $1::uuid`, sub).Scan(&cardID); err != nil {
		t.Fatal(err)
	}

	if err := m.rejectCard(ctx, cardID, "", "fake card"); err != nil {
		t.Fatalf("rejectCard: %v", err)
	}

	// Ledger nets to exactly 0 for the trial (both entries are amount 0 by
	// construction — card-trial is never a charge — so this also proves the
	// reversing entry was actually written, not just implicitly zero).
	var n int
	_ = db.QueryRow(ctx, `SELECT count(*) FROM ledger_transactions WHERE subscriber_id = $1::uuid AND source = 'card-reject'`, sub).Scan(&n)
	if n != 1 {
		t.Fatalf("expected 1 reversing ledger entry, got %d", n)
	}
	var sum int64
	_ = db.QueryRow(ctx, `SELECT COALESCE(sum(amount_iqd),0) FROM ledger_transactions WHERE subscriber_id = $1::uuid`, sub).Scan(&sum)
	if sum != 0 {
		t.Fatalf("ledger sum for subscriber = %d, want 0 (nets to zero)", sum)
	}

	// Subscriber lands expired again (was already expired pre-trial; the
	// 1-day trial's rollback floors at now, so status flips back to expired).
	var status string
	_ = db.QueryRow(ctx, `SELECT status FROM subscribers WHERE id = $1::uuid`, sub).Scan(&status)
	if status != "expired" {
		t.Fatalf("status after reject = %q, want expired", status)
	}

	// Cooldown: an immediate re-submission is blocked.
	_, err := m.submitCard(ctx, sub, "asiacell", "3333-4444-5555")
	if _, ok := err.(*cardCooldownError); !ok {
		t.Fatalf("expected *cardCooldownError, got %T: %v", err, err)
	}
}
