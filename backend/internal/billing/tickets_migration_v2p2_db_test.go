package billing_test

// Gate item 1 (v2-2): the card_payments -> payment_tickets migration (0586)
// is LOSSLESS. Mirrors internal/subscribers/migration_v2p1_db_test.go's
// scratch-DB pattern exactly: migrate a throwaway database to the last
// pre-this-migration version, write v1/v2-shaped card_payments rows (all
// three states), migrate to head, assert every row survived with the right
// method_key/method_detail/state/decision fields, one synthesized
// 'submitted' event per row, and card_payments gone.

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5/pgxpool"
)

// preTicketsMigration is 0585 — the last version before 0586 backfills
// card_payments into payment_tickets and drops the old table.
const preTicketsMigration = 585

// ticketsHeadMigration is the highest migration this phase adds.
const ticketsHeadMigration = 588

func ticketsMigrateURL(dbURL string) string {
	for _, scheme := range []string{"postgres://", "postgresql://"} {
		if strings.HasPrefix(dbURL, scheme) {
			return "pgx5://" + strings.TrimPrefix(dbURL, scheme)
		}
	}
	return dbURL
}

func ticketsWithScratchDB(t *testing.T) string {
	t.Helper()
	base := os.Getenv("HIKRAD_TEST_DB_URL")
	if base == "" {
		t.Skip("HIKRAD_TEST_DB_URL not set; skipping migration suite")
	}
	u, err := url.Parse(base)
	if err != nil {
		t.Fatalf("parse HIKRAD_TEST_DB_URL: %v", err)
	}
	name := fmt.Sprintf("hikrad_tix_mig_%d", rand.Int31())

	admin := *u
	admin.Path = "/postgres"
	ctx := context.Background()
	adminPool, err := pgxpool.New(ctx, admin.String())
	if err != nil {
		t.Skipf("cannot reach the postgres maintenance database: %v", err)
	}
	defer adminPool.Close()
	if _, err := adminPool.Exec(ctx, `CREATE DATABASE `+name); err != nil {
		t.Skipf("cannot create a scratch database (needs CREATEDB): %v", err)
	}
	t.Cleanup(func() {
		c, err := pgxpool.New(context.Background(), admin.String())
		if err != nil {
			return
		}
		defer c.Close()
		_, _ = c.Exec(context.Background(),
			`SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = $1`, name)
		_, _ = c.Exec(context.Background(), `DROP DATABASE IF EXISTS `+name)
	})

	scratch := *u
	scratch.Path = "/" + name
	return scratch.String()
}

func ticketsMigrator(t *testing.T, dbURL string) *migrate.Migrate {
	t.Helper()
	dir, err := filepath.Abs("../../migrations")
	if err != nil {
		t.Fatal(err)
	}
	m, err := migrate.New("file://"+filepath.ToSlash(dir), ticketsMigrateURL(dbURL))
	if err != nil {
		t.Fatalf("open migrations: %v", err)
	}
	return m
}

func TestCardPaymentsMigrationLossless(t *testing.T) {
	dbURL := ticketsWithScratchDB(t)
	ctx := context.Background()
	_ = slog.New(slog.NewTextHandler(io.Discard, nil))

	// 1. Bring the scratch DB to just before the migration under test.
	m := ticketsMigrator(t, dbURL)
	if err := m.Migrate(preTicketsMigration); err != nil {
		t.Fatalf("migrate to %d: %v", preTicketsMigration, err)
	}
	if srcErr, dbErr := m.Close(); srcErr != nil || dbErr != nil {
		t.Fatalf("close migrator: %v / %v", srcErr, dbErr)
	}

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("connect scratch: %v", err)
	}
	defer pool.Close()

	// 2. A profile + three subscribers, one per pre-migration card_payments state.
	var profileID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO profiles (name, price, currency, duration_days, rate_down_kbps, rate_up_kbps)
		 VALUES ('mig-plan', 15000, 'IQD', 30, 10240, 2048) RETURNING id::text`).Scan(&profileID); err != nil {
		t.Fatalf("insert profile: %v", err)
	}

	newSub := func(username string) string {
		var id string
		if err := pool.QueryRow(ctx,
			`INSERT INTO subscribers (username, password_enc, profile_id, status)
			 VALUES ($1, '\x0102'::bytea, $2::uuid, 'active') RETURNING id::text`,
			username, profileID).Scan(&id); err != nil {
			t.Fatalf("insert subscriber %s: %v", username, err)
		}
		return id
	}

	// A trial ledger row (amount always 0, chargeBalance:false) + the linked
	// payments row carrying the REAL resolved price — the shape 0586's own
	// join depends on (see its doc comment).
	newTrial := func(subID string, amount int64) (ledgerTxID string) {
		if err := pool.QueryRow(ctx,
			`INSERT INTO ledger_transactions (type, amount, currency, subscriber_id, source)
			 VALUES ('adjustment', 0, 'IQD', $1::uuid, 'card-trial') RETURNING id::text`, subID).
			Scan(&ledgerTxID); err != nil {
			t.Fatalf("insert trial ledger row: %v", err)
		}
		if _, err := pool.Exec(ctx,
			`INSERT INTO payments (receipt_no, ledger_tx_id, subscriber_id, amount, currency, method, source, share_token)
			 VALUES ($1, $2::uuid, $3::uuid, $4, 'IQD', 'card-trial', 'card-trial', $5)`,
			"MIG-"+ledgerTxID[:8], ledgerTxID, subID, amount, "share-"+ledgerTxID[:8]); err != nil {
			t.Fatalf("insert trial payment row: %v", err)
		}
		return ledgerTxID
	}

	subPending := newSub("mig_pending")
	subApproved := newSub("mig_approved")
	subRejected := newSub("mig_rejected")

	trialPending := newTrial(subPending, 15000)
	trialApproved := newTrial(subApproved, 15000)
	trialRejected := newTrial(subRejected, 15000)

	var approveLedgerID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO ledger_transactions (type, amount, currency, subscriber_id, source)
		 VALUES ('renewal', 15000, 'IQD', $1::uuid, 'card-zain') RETURNING id::text`, subApproved).
		Scan(&approveLedgerID); err != nil {
		t.Fatalf("insert approve ledger row: %v", err)
	}

	codeEnc := []byte("fake-encrypted-code")
	insertCard := func(id, subID, cardType, state string, trialTx string, approveTx *string, decidedBy *string, rejectReason *string) {
		if _, err := pool.Exec(ctx,
			`INSERT INTO card_payments (id, subscriber_id, profile_id, card_type, card_code_enc, state,
			                            trial_ledger_tx_id, approve_ledger_tx_id, decided_by, decided_at, reject_reason)
			 VALUES ($1::uuid, $2::uuid, $3::uuid, $4, $5, $6, $7::uuid, $8::uuid, $9::uuid,
			         CASE WHEN $6 != 'pending' THEN now() ELSE NULL END, $10)`,
			id, subID, profileID, cardType, codeEnc, state, trialTx, approveTx, decidedBy, rejectReason); err != nil {
			t.Fatalf("insert card_payments %s: %v", id, err)
		}
	}

	reason := "bad screenshot"
	insertCard(newUUIDLike(t, pool), subPending, "zain", "pending", trialPending, nil, nil, nil)
	insertCard(newUUIDLike(t, pool), subApproved, "asiacell", "approved", trialApproved, &approveLedgerID, nil, nil)
	insertCard(newUUIDLike(t, pool), subRejected, "zain", "rejected", trialRejected, nil, nil, &reason)

	var cardCountBefore int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM card_payments`).Scan(&cardCountBefore); err != nil {
		t.Fatal(err)
	}
	if cardCountBefore != 3 {
		t.Fatalf("setup: expected 3 card_payments rows, got %d", cardCountBefore)
	}

	// 3. Upgrade to head.
	m2 := ticketsMigrator(t, dbURL)
	if err := m2.Migrate(ticketsHeadMigration); err != nil {
		t.Fatalf("migrate to head (%d): %v", ticketsHeadMigration, err)
	}
	if v, dirty, err := m2.Version(); err != nil || dirty || v != ticketsHeadMigration {
		t.Fatalf("post-migration version = %d dirty=%v err=%v; want %d clean", v, dirty, err, ticketsHeadMigration)
	}
	if srcErr, dbErr := m2.Close(); srcErr != nil || dbErr != nil {
		t.Fatalf("close migrator: %v / %v", srcErr, dbErr)
	}

	// 4a. Zero row loss.
	var ticketCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM payment_tickets WHERE method_key = 'scratch_card'`).
		Scan(&ticketCount); err != nil {
		t.Fatal(err)
	}
	if ticketCount != 3 {
		t.Fatalf("payment_tickets scratch_card rows = %d, want 3 (lost rows)", ticketCount)
	}

	// 4b. card_payments is gone.
	var tableExists bool
	if err := pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'card_payments')`).
		Scan(&tableExists); err != nil {
		t.Fatal(err)
	}
	if tableExists {
		t.Error("card_payments still exists after 0586")
	}

	// 4c. Each migrated row: correct method_detail, state, amount/currency
	// (sourced from the trial payment row, not the 0-amount ledger row), and
	// exactly one synthesized 'submitted' event.
	type want struct {
		subID, cardType, state string
		reason                 *string
	}
	for _, w := range []want{
		{subPending, "zain", "pending", nil},
		{subApproved, "asiacell", "approved", nil},
		{subRejected, "zain", "rejected", &reason},
	} {
		var id, state, methodDetail string
		var amount int64
		var currency string
		var gotReason *string
		if err := pool.QueryRow(ctx,
			`SELECT id::text, state, method_detail::text, amount, currency, reject_reason
			   FROM payment_tickets WHERE subscriber_id = $1::uuid`, w.subID).
			Scan(&id, &state, &methodDetail, &amount, &currency, &gotReason); err != nil {
			t.Fatalf("read migrated ticket for %s: %v", w.subID, err)
		}
		if state != w.state {
			t.Errorf("%s: state = %q, want %q", w.subID, state, w.state)
		}
		if amount != 15000 || currency != "IQD" {
			t.Errorf("%s: amount/currency = %d/%s, want 15000/IQD (from the trial PAYMENT row, not the 0-amount ledger row)",
				w.subID, amount, currency)
		}
		if !strings.Contains(methodDetail, w.cardType) {
			t.Errorf("%s: method_detail = %s, want it to carry card_type=%q", w.subID, methodDetail, w.cardType)
		}
		if !strings.Contains(methodDetail, base64.StdEncoding.EncodeToString(codeEnc)) {
			t.Errorf("%s: method_detail = %s, missing the base64 card_code_enc", w.subID, methodDetail)
		}
		if (w.reason == nil) != (gotReason == nil) {
			t.Errorf("%s: reject_reason = %v, want %v", w.subID, gotReason, w.reason)
		}

		var eventCount int
		var eventType string
		if err := pool.QueryRow(ctx,
			`SELECT count(*), min(event_type) FROM payment_ticket_events WHERE ticket_id = $1::uuid`, id).
			Scan(&eventCount, &eventType); err != nil {
			t.Fatal(err)
		}
		if eventCount != 1 || eventType != "submitted" {
			t.Errorf("%s: %d timeline events (type %q), want exactly 1 synthesized 'submitted'", w.subID, eventCount, eventType)
		}
	}
}

// newUUIDLike lets the DB generate a fresh uuid for card_payments.id (the
// table's own default) rather than hand-rolling one client-side.
func newUUIDLike(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	var id string
	if err := pool.QueryRow(context.Background(), `SELECT gen_random_uuid()::text`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}
