package cards

// Gate item 8: "Every pre-existing code still redeems; count and codes
// identical pre/post." Migration 0605 already applied against an EMPTY
// voucher table when testDB(t) ran platform.Migrate (a fresh test DB has no
// pre-existing vouchers) — so this test seeds vouchers exactly as
// billing/voucher.go would have, at real ISP install time, and then
// re-executes 0605's own SQL text directly against the now-populated tables
// to prove what it does on an upgrade with real voucher history.
//
// Like the accounting chaos suite's own documented flakiness
// (docs/ops/known-issues.md Wontfix: "passes on a fresh DB, fails on a
// second run"), this test is not safe to re-run against an already-mutated
// database: golang-migrate only ever applies 0605 once in production, so
// manually re-executing it here a second time collides on the primary keys
// this test's OWN first run already inserted. Run against a fresh database.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// seedLegacyVoucher writes one voucher_batches + vouchers row using the
// IDENTICAL columns billing/voucher.go's generateBatch/insertCodes populate,
// without importing billing (avoiding a cross-package test dependency for a
// handful of INSERTs) — code_hash uses the same sha256(normalized) scheme
// both packages share.
func seedLegacyVoucher(t *testing.T, db *pgxpool.Pool, profileID, prefix, code string, used bool) (batchID, voucherID, codeHash string) {
	t.Helper()
	ctx := context.Background()
	if err := db.QueryRow(ctx,
		`INSERT INTO voucher_batches (profile_id, prefix, count, unit_price, currency, state)
		 VALUES ($1::uuid, $2, 1, 5000, 'IQD', 'active') RETURNING id::text`,
		profileID, prefix).Scan(&batchID); err != nil {
		t.Fatalf("seed voucher_batches: %v", err)
	}
	codeHash = legacyTestHash(code)
	state := "unused"
	if used {
		state = "used"
	}
	if err := db.QueryRow(ctx,
		`INSERT INTO vouchers (batch_id, code_hash, state, used_at)
		 VALUES ($1::uuid, $2, $3, CASE WHEN $3 = 'used' THEN now() ELSE NULL END)
		 RETURNING id::text`,
		batchID, codeHash, state).Scan(&voucherID); err != nil {
		t.Fatalf("seed vouchers: %v", err)
	}
	return batchID, voucherID, codeHash
}

func legacyTestHash(code string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(code) {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

func countVouchers(t *testing.T, db *pgxpool.Pool) int {
	t.Helper()
	var n int
	if err := db.QueryRow(context.Background(), `SELECT count(*) FROM vouchers`).Scan(&n); err != nil {
		t.Fatalf("count vouchers: %v", err)
	}
	return n
}

// voucherFingerprint hashes every column of one voucher row so the test can
// assert "byte-for-byte unchanged" without hand-listing every field twice.
func voucherFingerprint(t *testing.T, db *pgxpool.Pool, voucherID string) string {
	t.Helper()
	var fp string
	if err := db.QueryRow(context.Background(),
		`SELECT md5(row(v.*)::text) FROM vouchers v WHERE id = $1::uuid`, voucherID).Scan(&fp); err != nil {
		t.Fatalf("fingerprint voucher %s: %v", voucherID, err)
	}
	return fp
}

func TestMigrateVouchersToCards_LosslessAndAdditiveOnly(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	var profileID string
	if err := db.QueryRow(ctx,
		`INSERT INTO profiles (name, price, currency, duration_days, rate_down_kbps, rate_up_kbps)
		 VALUES ($1, 5000, 'IQD', 30, 1024, 1024) RETURNING id::text`, uniqName("plan")).Scan(&profileID); err != nil {
		t.Fatalf("seed profile: %v", err)
	}

	prefix := uniqName("VCH")
	code := prefix + "1234567"
	batchID, voucherID, codeHash := seedLegacyVoucher(t, db, profileID, prefix, code, false)
	usedCode := prefix + "USED0001"
	_, usedVoucherID, usedHash := seedLegacyVoucher(t, db, profileID, prefix, usedCode, true)

	// Snapshot the voucher tables BEFORE running the migration SQL.
	beforeCount := countVouchers(t, db)
	beforeFP := voucherFingerprint(t, db, voucherID)
	beforeUsedFP := voucherFingerprint(t, db, usedVoucherID)

	migrationSQL, err := os.ReadFile("../../migrations/0605_v4_5_migrate_vouchers_to_cards.up.sql")
	if err != nil {
		t.Fatalf("read migration file: %v", err)
	}
	if _, err := db.Exec(ctx, string(migrationSQL)); err != nil {
		t.Fatalf("re-run migration 0605 against seeded vouchers: %v", err)
	}

	// 1. vouchers/voucher_batches are BYTE-FOR-BYTE unchanged (additive-only —
	// the whole point of gate item 8's "no voucher code changes").
	if afterCount := countVouchers(t, db); afterCount != beforeCount {
		t.Fatalf("voucher row count changed: before=%d after=%d", beforeCount, afterCount)
	}
	if got := voucherFingerprint(t, db, voucherID); got != beforeFP {
		t.Fatal("unused voucher row was modified by the migration")
	}
	if got := voucherFingerprint(t, db, usedVoucherID); got != beforeUsedFP {
		t.Fatal("used voucher row was modified by the migration")
	}

	// 2. A synthetic card_groups row exists with the SAME id as the batch.
	var groupExists bool
	if err := db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM card_groups WHERE id = $1::uuid AND source = 'legacy_voucher')`,
		batchID).Scan(&groupExists); err != nil {
		t.Fatalf("query card_groups: %v", err)
	}
	if !groupExists {
		t.Fatal("no synthetic card_groups row for the legacy voucher batch")
	}

	// 3. A cards row exists with the IDENTICAL code_hash — byte-for-byte code
	// preservation (never re-hashed from any plaintext, since none exists).
	var gotHash, gotState string
	if err := db.QueryRow(ctx, `SELECT code_hash, state FROM cards WHERE id = $1::uuid`, voucherID).
		Scan(&gotHash, &gotState); err != nil {
		t.Fatalf("query cards (unused): %v", err)
	}
	if gotHash != codeHash {
		t.Fatalf("code_hash mismatch: voucher=%s card=%s", codeHash, gotHash)
	}
	if gotState != "unused" {
		t.Fatalf("unused voucher mapped to card state %q, want unused", gotState)
	}

	var usedGotHash, usedGotState string
	if err := db.QueryRow(ctx, `SELECT code_hash, state FROM cards WHERE id = $1::uuid`, usedVoucherID).
		Scan(&usedGotHash, &usedGotState); err != nil {
		t.Fatalf("query cards (used): %v", err)
	}
	if usedGotHash != usedHash {
		t.Fatalf("code_hash mismatch (used): voucher=%s card=%s", usedHash, usedGotHash)
	}
	if usedGotState != "exhausted" {
		t.Fatalf("used voucher mapped to card state %q, want exhausted", usedGotState)
	}
}
