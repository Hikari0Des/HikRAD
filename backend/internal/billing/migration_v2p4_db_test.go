package billing_test

// Gate item 1 (v2 phase 4, mirrors v2 phase 1's gate item 1 pattern exactly):
// the currency migrations (0530-0538) are LOSSLESS against real v1 data.
//
// 0532/0533/0535/0536/0537 each RENAME a *_iqd column rather than drop it, so
// this is lower-stakes than phase 1's column drops — but the composite-PK
// change on manager_balances (migration 0533: PRIMARY KEY (manager_id) ->
// PRIMARY KEY (manager_id, currency)) is exactly the kind of structural change
// that silently loses data if done wrong, so it gets the same scratch-database
// treatment: migrate to the last pre-phase-4 migration, write v1-shaped rows
// with no currency concept, migrate to head, and assert every value survived
// under its new name/shape with currency defaulted to 'IQD'.

import (
	"context"
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

// lastPreV2P4Migration is the highest migration before this phase's range
// (0530+). Migrating to exactly this version reproduces a real v1 install.
const lastPreV2P4Migration = 520

// v2p4Head is the highest migration this phase adds.
const v2p4Head = 538

func migV2P4URL(dbURL string) string {
	for _, scheme := range []string{"postgres://", "postgresql://"} {
		if strings.HasPrefix(dbURL, scheme) {
			return "pgx5://" + strings.TrimPrefix(dbURL, scheme)
		}
	}
	return dbURL
}

func withV2P4ScratchDB(t *testing.T) string {
	t.Helper()
	base := os.Getenv("HIKRAD_TEST_DB_URL")
	if base == "" {
		t.Skip("HIKRAD_TEST_DB_URL not set; skipping migration suite")
	}
	u, err := url.Parse(base)
	if err != nil {
		t.Fatalf("parse HIKRAD_TEST_DB_URL: %v", err)
	}
	name := fmt.Sprintf("hikrad_mig4_%d", rand.Int31())

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

func v2p4Migrator(t *testing.T, dbURL string) *migrate.Migrate {
	t.Helper()
	dir, err := filepath.Abs("../../migrations")
	if err != nil {
		t.Fatal(err)
	}
	m, err := migrate.New("file://"+filepath.ToSlash(dir), migV2P4URL(dbURL))
	if err != nil {
		t.Fatalf("open migrations: %v", err)
	}
	return m
}

func TestCurrencyMigrationLossless(t *testing.T) {
	dbURL := withV2P4ScratchDB(t)
	ctx := context.Background()
	_ = slog.New(slog.NewTextHandler(io.Discard, nil))

	// 1. Bring the scratch DB to a real pre-phase-4 install's schema.
	m := v2p4Migrator(t, dbURL)
	if err := m.Migrate(lastPreV2P4Migration); err != nil {
		t.Fatalf("migrate to pre-v2-4 head (%d): %v", lastPreV2P4Migration, err)
	}
	if srcErr, dbErr := m.Close(); srcErr != nil || dbErr != nil {
		t.Fatalf("close migrator: %v / %v", srcErr, dbErr)
	}

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("connect scratch: %v", err)
	}
	defer pool.Close()

	// 2. Write rows in the pre-currency shape: bare *_iqd integers, no currency
	// column anywhere — exactly what an upgrading customer's database holds.
	var mgrID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO managers (username, password_hash, role) VALUES ('v1_mgr', 'x', 'agent') RETURNING id::text`).
		Scan(&mgrID); err != nil {
		t.Fatalf("insert manager: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO manager_balances (manager_id, balance_iqd) VALUES ($1::uuid, 42000)`, mgrID); err != nil {
		t.Fatalf("insert manager_balances: %v", err)
	}
	var ledgerTxID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO ledger_transactions (type, amount_iqd, actor_manager_id, source)
		 VALUES ('topup', 42000, $1::uuid, 'panel') RETURNING id::text`, mgrID).Scan(&ledgerTxID); err != nil {
		t.Fatalf("insert ledger_transactions: %v", err)
	}
	var profID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO profiles (name, price_iqd, duration_days, rate_down_kbps, rate_up_kbps)
		 VALUES ('v1 plan', 25000, 30, 10240, 2048) RETURNING id::text`).Scan(&profID); err != nil {
		t.Fatalf("insert profiles: %v", err)
	}
	var batchID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO voucher_batches (profile_id, count, unit_price_iqd) VALUES ($1::uuid, 5, 25000) RETURNING id::text`,
		profID).Scan(&batchID); err != nil {
		t.Fatalf("insert voucher_batches: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO payments (receipt_no, ledger_tx_id, amount_iqd, method, source)
		 VALUES ('HR-000001', $1::uuid, 25000, 'topup', 'panel')`, ledgerTxID); err != nil {
		t.Fatalf("insert payments: %v", err)
	}

	// 3. Upgrade to this phase's head — what `hikrad update` does on boot.
	m2 := v2p4Migrator(t, dbURL)
	if err := m2.Migrate(v2p4Head); err != nil {
		t.Fatalf("migrate to v2 phase-4 head (%d): %v", v2p4Head, err)
	}
	if v, dirty, err := m2.Version(); err != nil || dirty || v != v2p4Head {
		t.Fatalf("post-migration version = %d dirty=%v err=%v; want %d clean", v, dirty, err, v2p4Head)
	}
	if srcErr, dbErr := m2.Close(); srcErr != nil || dbErr != nil {
		t.Fatalf("close migrator: %v / %v", srcErr, dbErr)
	}

	// 4a. manager_balances: value survived under the new column name, and the
	// new composite PK (manager_id, currency) holds the same balance under
	// currency='IQD' as the old manager_id-only PK held.
	var bal int64
	var cur string
	if err := pool.QueryRow(ctx,
		`SELECT balance, currency FROM manager_balances WHERE manager_id = $1::uuid`, mgrID).
		Scan(&bal, &cur); err != nil {
		t.Fatalf("read manager_balances: %v", err)
	}
	if bal != 42000 || cur != "IQD" {
		t.Errorf("manager_balances = (%d, %q), want (42000, IQD) — the migration lost or mis-tagged the balance", bal, cur)
	}

	// 4b. ledger_transactions: amount_iqd -> amount, currency defaulted to IQD.
	var ledgerAmount int64
	var ledgerCur string
	if err := pool.QueryRow(ctx,
		`SELECT amount, currency FROM ledger_transactions WHERE actor_manager_id = $1::uuid`, mgrID).
		Scan(&ledgerAmount, &ledgerCur); err != nil {
		t.Fatalf("read ledger_transactions: %v", err)
	}
	if ledgerAmount != 42000 || ledgerCur != "IQD" {
		t.Errorf("ledger_transactions = (%d, %q), want (42000, IQD)", ledgerAmount, ledgerCur)
	}

	// 4c. profiles: price_iqd -> price, currency defaulted to IQD.
	var price int64
	var priceCur string
	if err := pool.QueryRow(ctx,
		`SELECT price, currency FROM profiles WHERE id = $1::uuid`, profID).Scan(&price, &priceCur); err != nil {
		t.Fatalf("read profiles: %v", err)
	}
	if price != 25000 || priceCur != "IQD" {
		t.Errorf("profiles = (%d, %q), want (25000, IQD)", price, priceCur)
	}

	// 4d. voucher_batches: unit_price_iqd -> unit_price, currency defaulted.
	var unitPrice int64
	var unitCur string
	if err := pool.QueryRow(ctx,
		`SELECT unit_price, currency FROM voucher_batches WHERE id = $1::uuid`, batchID).
		Scan(&unitPrice, &unitCur); err != nil {
		t.Fatalf("read voucher_batches: %v", err)
	}
	if unitPrice != 25000 || unitCur != "IQD" {
		t.Errorf("voucher_batches = (%d, %q), want (25000, IQD)", unitPrice, unitCur)
	}

	// 4e. payments: amount_iqd -> amount, currency defaulted.
	var payAmount int64
	var payCur string
	if err := pool.QueryRow(ctx,
		`SELECT amount, currency FROM payments WHERE receipt_no = 'HR-000001'`).Scan(&payAmount, &payCur); err != nil {
		t.Fatalf("read payments: %v", err)
	}
	if payAmount != 25000 || payCur != "IQD" {
		t.Errorf("payments = (%d, %q), want (25000, IQD)", payAmount, payCur)
	}

	// 4f. The renamed columns are gone (no *_iqd survivors to drift out of sync).
	for _, tc := range []struct{ table, col string }{
		{"manager_balances", "balance_iqd"},
		{"ledger_transactions", "amount_iqd"},
		{"profiles", "price_iqd"},
		{"voucher_batches", "unit_price_iqd"},
		{"payments", "amount_iqd"},
	} {
		var exists bool
		if err := pool.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM information_schema.columns
			                 WHERE table_name=$1 AND column_name=$2)`, tc.table, tc.col).Scan(&exists); err != nil {
			t.Fatal(err)
		}
		if exists {
			t.Errorf("%s.%s still exists after the phase-4 migrations", tc.table, tc.col)
		}
	}

	// 4g. currencies catalog seeded (C1): IQD/USD/EUR, all enabled.
	var currencyCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM currencies WHERE enabled`).Scan(&currencyCount); err != nil {
		t.Fatal(err)
	}
	if currencyCount < 3 {
		t.Errorf("currencies catalog has %d enabled rows, want >= 3 (IQD/USD/EUR)", currencyCount)
	}
}
