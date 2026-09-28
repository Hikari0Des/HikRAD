package billing_test

// v4-9 (FR-153-159) DB-gated suite. Reuses env/setup/e.do/e.login/
// e.createProfile/e.createSubscriber/e.seedAgent from db_test.go (same
// package). Gated on HIKRAD_TEST_DB_URL like every other DB-backed suite in
// this repo; self-skips cleanly when unset.

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"testing"
	"time"
)

// grantOverride inserts a manager_permission_overrides row. Must run BEFORE
// the manager's login (permissions are resolved into the access token at
// login time, not re-checked per request).
func (e env) grantOverride(t *testing.T, managerID, perm string) {
	t.Helper()
	if _, err := e.db.Exec(context.Background(),
		`INSERT INTO manager_permission_overrides (manager_id, permission, granted) VALUES ($1::uuid,$2,true)
		 ON CONFLICT (manager_id, permission) DO UPDATE SET granted = true`, managerID, perm); err != nil {
		t.Fatalf("grant override: %v", err)
	}
}

func (e env) setCreditLimit(t *testing.T, managerID string, limit int64) {
	t.Helper()
	if _, err := e.db.Exec(context.Background(), `UPDATE managers SET credit_limit = $2 WHERE id = $1::uuid`, managerID, limit); err != nil {
		t.Fatalf("set credit limit: %v", err)
	}
}

func (e env) setCommission(t *testing.T, managerID string, pct float64) {
	t.Helper()
	if _, err := e.db.Exec(context.Background(), `UPDATE managers SET commission_pct = $2 WHERE id = $1::uuid`, managerID, pct); err != nil {
		t.Fatalf("set commission: %v", err)
	}
}

// walletJournalNet returns SUM(credit-debit) on the Agent Prepaid Balances
// (2100) account tagged to managerID — the journal's own idea of that
// manager's wallet balance, computed independently of manager_balances.
func (e env) walletJournalNet(t *testing.T, managerID string) int64 {
	t.Helper()
	var n int64
	if err := e.db.QueryRow(context.Background(),
		`SELECT COALESCE(sum(jl.credit - jl.debit),0)
		   FROM journal_lines jl JOIN accounts a ON a.id = jl.account_id
		  WHERE a.code = '2100' AND jl.manager_id = $1::uuid`, managerID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func (e env) journalEntryLineCount(t *testing.T, entryID string) int {
	t.Helper()
	var n int
	if err := e.db.QueryRow(context.Background(), `SELECT count(*) FROM journal_lines WHERE entry_id = $1::uuid`, entryID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// --- gate item 1: entries balance or are refused --------------------------

func TestJournal_UnbalancedEntryRefused(t *testing.T) {
	e := setup(t)
	ctx := context.Background()

	var cashID, revenueID string
	if err := e.db.QueryRow(ctx, `SELECT id FROM accounts WHERE code='1000'`).Scan(&cashID); err != nil {
		t.Fatal(err)
	}
	if err := e.db.QueryRow(ctx, `SELECT id FROM accounts WHERE code='4000'`).Scan(&revenueID); err != nil {
		t.Fatal(err)
	}

	tx, err := e.db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)

	var entryID string
	if err := tx.QueryRow(ctx, `INSERT INTO journal_entries (memo, source, reference) VALUES ('test-unbalanced','test','') RETURNING id`).Scan(&entryID); err != nil {
		t.Fatal(err)
	}
	// A deliberately unbalanced pair: debit 100 vs credit 50. This bypasses
	// postJournal's Go-side guard entirely (raw SQL) — only the migration
	// 0617 DB trigger stands between this and a committed unbalanced entry.
	if _, err := tx.Exec(ctx, `INSERT INTO journal_lines (entry_id, account_id, debit, credit) VALUES ($1,$2,100,0)`, entryID, cashID); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO journal_lines (entry_id, account_id, debit, credit) VALUES ($1,$2,0,50)`, entryID, revenueID); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err == nil {
		t.Fatal("want the commit to fail on an unbalanced journal entry, but it succeeded")
	}
}

func TestJournal_TwoLineMinimumEnforced(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	var cashID string
	if err := e.db.QueryRow(ctx, `SELECT id FROM accounts WHERE code='1000'`).Scan(&cashID); err != nil {
		t.Fatal(err)
	}
	tx, err := e.db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	var entryID string
	if err := tx.QueryRow(ctx, `INSERT INTO journal_entries (memo, source, reference) VALUES ('test-single-line','test','') RETURNING id`).Scan(&entryID); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO journal_lines (entry_id, account_id, debit, credit) VALUES ($1,$2,100,0)`, entryID, cashID); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err == nil {
		t.Fatal("want the commit to fail on a single-line entry (trivially 'balanced' at zero credit), but it succeeded")
	}
}

// --- gate item 3: dual-write agrees ----------------------------------------

func TestJournal_DualWriteAgrees_TopupRenewRefund(t *testing.T) {
	e := setup(t)
	agentID, agentToken := e.seedAgent(t)
	prof := e.createProfile(t, 15000, 30)
	sub := e.createSubscriber(t, prof)
	// v4-12 (FR-175): must be owned by the agent — a scoped agent's own
	// /renew call 404s "subscriber not found" against an unowned
	// subscriber (ScopeFilter hides it), which is why this test never
	// reached its own reconciliation assertions below. Found while
	// verifying a real sign bug in postLedgerJournal's wallet-mediated
	// case (journal.go) that this exact test exists to catch — same
	// ownership-assignment pattern every other test in this file already
	// uses. See docs/ops/known-issues.md.
	if _, err := e.db.Exec(context.Background(),
		`UPDATE subscribers SET owner_manager_id=$1::uuid WHERE id=$2::uuid`, agentID, sub); err != nil {
		t.Fatalf("assign owner: %v", err)
	}

	// Top up the agent's wallet.
	r := e.do(t, "POST", "/api/v1/managers/"+agentID+"/topup", e.token, map[string]any{"amount": 100000})
	if r.status != http.StatusOK {
		t.Fatalf("topup = %d: %s", r.status, r.body)
	}

	// Agent renews (spends wallet).
	r = e.do(t, "POST", "/api/v1/subscribers/"+sub+"/renew", agentToken, map[string]any{})
	if r.status != http.StatusOK {
		t.Fatalf("renew = %d: %s", r.status, r.body)
	}
	var renewRes struct {
		LedgerTxID string `json:"ledger_tx_id"`
	}
	r.into(t, &renewRes)

	if bal, wallet := e.cachedBalance(t, agentID), e.walletJournalNet(t, agentID); bal != wallet {
		t.Fatalf("after renew: manager_balances=%d journal wallet net=%d, want equal", bal, wallet)
	}
	if ledger, wallet := e.ledgerSum(t, agentID), e.walletJournalNet(t, agentID); ledger != wallet {
		t.Fatalf("after renew: ledger sum=%d journal wallet net=%d, want equal", ledger, wallet)
	}

	// Refund the renewal (reverses).
	r = e.do(t, "POST", "/api/v1/subscribers/"+sub+"/refund", e.token, map[string]any{
		"ledger_tx_id": renewRes.LedgerTxID, "reason": "test refund",
	})
	if r.status != http.StatusOK {
		t.Fatalf("refund = %d: %s", r.status, r.body)
	}

	if bal, wallet := e.cachedBalance(t, agentID), e.walletJournalNet(t, agentID); bal != wallet {
		t.Fatalf("after refund: manager_balances=%d journal wallet net=%d, want equal", bal, wallet)
	}
	if ledger, wallet := e.ledgerSum(t, agentID), e.walletJournalNet(t, agentID); ledger != wallet {
		t.Fatalf("after refund: ledger sum=%d journal wallet net=%d, want equal", ledger, wallet)
	}
}

// --- gate item 2: backfill reconciles --------------------------------------

func TestJournal_BackfillReconciles(t *testing.T) {
	e := setup(t)
	ctx := context.Background()

	agentID, _ := e.seedAgent(t)

	// Simulate "pre-v4-9 historical data": raw ledger_transactions rows
	// inserted directly, bypassing insertLedger/postLedgerJournal entirely
	// — exactly the shape a real upgrade's existing rows would have (no
	// journal_entries counterpart yet).
	var topupID string
	if err := e.db.QueryRow(ctx,
		`INSERT INTO ledger_transactions (type, amount, currency, actor_manager_id, source, reference, note)
		 VALUES ('topup', 50000, 'IQD', $1::uuid, 'migration-test', '', 'synthetic historical topup')
		 RETURNING id`, agentID).Scan(&topupID); err != nil {
		t.Fatal(err)
	}
	var renewalID string
	if err := e.db.QueryRow(ctx,
		`INSERT INTO ledger_transactions (type, amount, currency, actor_manager_id, source, reference, note, cost_at_sale)
		 VALUES ('renewal', -12000, 'IQD', $1::uuid, 'migration-test', '', 'synthetic historical renewal', 3000)
		 RETURNING id`, agentID).Scan(&renewalID); err != nil {
		t.Fatal(err)
	}

	// Re-run migration 0618's backfill SQL against these freshly-inserted
	// synthetic "historical" rows — a faithful proxy for what a real
	// upgrade's ONE-TIME run of this migration does against pre-existing
	// data. golang-migrate itself never re-runs a migration file twice
	// against one database (it tracks applied versions), so re-running the
	// raw file here is test-only; migration 0618's bare `CREATE TABLE
	// journal_backfill_report` (correct for its one real run) would collide
	// with the copy `setup()`'s own platform.Migrate already created, so the
	// test drops that report table first — the backfill logic that matters
	// (NOT EXISTS-guarded per journal entry) is otherwise untouched and
	// still exercised exactly as shipped.
	if _, err := e.db.Exec(ctx, `DROP TABLE IF EXISTS journal_backfill_report`); err != nil {
		t.Fatalf("drop journal_backfill_report before re-run: %v", err)
	}
	sql, err := os.ReadFile("../../migrations/0618_v4_9_journal_backfill.up.sql")
	if err != nil {
		t.Fatalf("read backfill migration: %v", err)
	}
	if _, err := e.db.Exec(ctx, string(sql)); err != nil {
		t.Fatalf("re-run backfill: %v", err)
	}

	// Every synthetic row now has a journal entry.
	var n int
	if err := e.db.QueryRow(ctx, `SELECT count(*) FROM journal_entries WHERE ledger_tx_id IN ($1::uuid,$2::uuid)`, topupID, renewalID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("want 2 journal entries backfilled for the synthetic rows, got %d", n)
	}

	// Reconciliation: manager's ledger-derived total (over actor-bearing
	// rows) equals the journal's wallet-account net for that manager.
	var ledgerTotal int64
	if err := e.db.QueryRow(ctx,
		`SELECT COALESCE(sum(amount),0) FROM ledger_transactions WHERE actor_manager_id = $1::uuid AND currency='IQD'`, agentID).
		Scan(&ledgerTotal); err != nil {
		t.Fatal(err)
	}
	if got := e.walletJournalNet(t, agentID); got != ledgerTotal {
		t.Fatalf("backfill reconciliation: ledger total=%d journal wallet net=%d, want equal", ledgerTotal, got)
	}

	// The backfill report row itself was written.
	var reportRows int
	if err := e.db.QueryRow(ctx, `SELECT count(*) FROM journal_backfill_report`).Scan(&reportRows); err != nil {
		t.Fatal(err)
	}
	if reportRows == 0 {
		t.Fatal("want at least one journal_backfill_report row")
	}
}

// --- gate item 6: statements reconcile -------------------------------------

func TestStatements_TrialBalanceNetsToZero(t *testing.T) {
	e := setup(t)
	agentID, agentToken := e.seedAgent(t)
	prof := e.createProfile(t, 10000, 30)
	sub := e.createSubscriber(t, prof)
	e.do(t, "POST", "/api/v1/managers/"+agentID+"/topup", e.token, map[string]any{"amount": 50000})
	e.do(t, "POST", "/api/v1/subscribers/"+sub+"/renew", agentToken, map[string]any{})

	r := e.do(t, "GET", "/api/v1/statements/trial-balance", e.token, nil)
	if r.status != http.StatusOK {
		t.Fatalf("trial balance = %d: %s", r.status, r.body)
	}
	var out struct {
		TotalDebit  int64 `json:"total_debit"`
		TotalCredit int64 `json:"total_credit"`
	}
	r.into(t, &out)
	if out.TotalDebit != out.TotalCredit {
		t.Fatalf("trial balance does not net to zero: debit=%d credit=%d", out.TotalDebit, out.TotalCredit)
	}
	if out.TotalDebit == 0 {
		t.Fatal("test setup produced no journal activity to check")
	}
}

func TestStatements_BalanceSheetBalances(t *testing.T) {
	e := setup(t)
	agentID, agentToken := e.seedAgent(t)
	prof := e.createProfile(t, 10000, 30)
	sub := e.createSubscriber(t, prof)
	e.do(t, "POST", "/api/v1/managers/"+agentID+"/topup", e.token, map[string]any{"amount": 50000})
	e.do(t, "POST", "/api/v1/subscribers/"+sub+"/renew", agentToken, map[string]any{})

	r := e.do(t, "GET", "/api/v1/statements/balance-sheet", e.token, nil)
	if r.status != http.StatusOK {
		t.Fatalf("balance sheet = %d: %s", r.status, r.body)
	}
	var out struct {
		Assets      int64 `json:"assets"`
		Liabilities int64 `json:"liabilities"`
		Equity      int64 `json:"equity"`
	}
	r.into(t, &out)
	if out.Assets != out.Liabilities+out.Equity {
		t.Fatalf("balance sheet does not balance: assets=%d liabilities+equity=%d", out.Assets, out.Liabilities+out.Equity)
	}
}

// --- gate item 12: reseller isolation ---------------------------------------

func TestStatements_ResellerIsolationOmitsCostFields(t *testing.T) {
	e := setup(t)
	agentID, agentToken := e.seedAgent(t)
	e.grantOverride(t, agentID, "accounting.view")
	prof := e.createProfile(t, 10000, 30)
	sub := e.createSubscriber(t, prof)
	e.do(t, "POST", "/api/v1/managers/"+agentID+"/topup", e.token, map[string]any{"amount": 50000})
	e.do(t, "POST", "/api/v1/subscribers/"+sub+"/renew", agentToken, map[string]any{})

	// Unscoped (admin) caller sees cost_of_service/gross_margin.
	r := e.do(t, "GET", "/api/v1/statements/pl", e.token, nil)
	var adminBody map[string]json.RawMessage
	r.into(t, &adminBody)
	if _, ok := adminBody["cost_of_service"]; !ok {
		t.Error("admin P&L response is missing cost_of_service — should be present")
	}

	// Scoped (agent) caller does NOT see them — key absent, not null.
	r2 := e.do(t, "GET", "/api/v1/statements/pl", agentReloginToken(t, e, agentID), nil)
	if r2.status != http.StatusOK {
		t.Fatalf("agent P&L = %d: %s", r2.status, r2.body)
	}
	var agentBody map[string]json.RawMessage
	r2.into(t, &agentBody)
	if _, ok := agentBody["cost_of_service"]; ok {
		t.Errorf("scoped caller's P&L response should OMIT cost_of_service entirely, got %s", agentBody["cost_of_service"])
	}
	if _, ok := agentBody["gross_margin"]; ok {
		t.Errorf("scoped caller's P&L response should OMIT gross_margin entirely, got %s", agentBody["gross_margin"])
	}
	if _, ok := agentBody["revenue"]; !ok {
		t.Error("scoped caller's P&L response should still include revenue")
	}
}

// agentReloginToken re-logs the seeded agent in (username stored on the
// manager row) so a permission grant made after their original login is
// reflected in a fresh token.
func agentReloginToken(t *testing.T, e env, managerID string) string {
	t.Helper()
	var username string
	if err := e.db.QueryRow(context.Background(), `SELECT username FROM managers WHERE id = $1::uuid`, managerID).Scan(&username); err != nil {
		t.Fatal(err)
	}
	return e.login(t, username, "agentpw")
}

// --- gate item 4: invoice modes --------------------------------------------

func TestInvoices_FullMode(t *testing.T) {
	e := setup(t)
	r := e.do(t, "POST", "/api/v1/invoices", e.token, map[string]any{
		"mode": "full", "due_date": "2026-09-01", "tax_rate": 10,
		"lines": []map[string]any{{"description": "Plan", "category": "subscription", "quantity": 1, "unit_price": 20000}},
	})
	if r.status != http.StatusCreated {
		t.Fatalf("create invoice = %d: %s", r.status, r.body)
	}
	// v4-12 (FR-175): json tags were missing — Go's untagged case-insensitive
	// field match doesn't bridge "tax_amount" (JSON) to "TaxAmount" (Go),
	// only case, so TaxAmount silently stayed 0 and this assertion never
	// actually checked the server's real (correct) tax computation.
	var v struct {
		Subtotal  int64  `json:"subtotal"`
		TaxAmount int64  `json:"tax_amount"`
		Total     int64  `json:"total"`
		Status    string `json:"status"`
	}
	r.into(t, &v)
	if v.Subtotal != 20000 || v.TaxAmount != 2000 || v.Total != 22000 {
		t.Fatalf("invoice totals = subtotal=%d tax=%d total=%d, want 20000/2000/22000", v.Subtotal, v.TaxAmount, v.Total)
	}
	if v.Status != "unpaid" {
		t.Fatalf("status = %q, want unpaid", v.Status)
	}
}

func TestInvoices_NoChargeRefusedWithoutPermission(t *testing.T) {
	e := setup(t)
	agentID, _ := e.seedAgent(t)
	// Grant invoices.create (so the route's own middleware lets the request
	// through) but deliberately NOT invoices.no_charge — the 403 below must
	// come from createInvoiceHandler's own no_charge check, not from
	// Require() rejecting the request before it ever reaches that check.
	e.grantOverride(t, agentID, "invoices.create")
	token := agentReloginToken(t, e, agentID)

	r := e.do(t, "POST", "/api/v1/invoices", token, map[string]any{
		"mode": "no_charge", "due_date": "2026-09-01",
		"lines": []map[string]any{{"description": "Waived", "quantity": 1, "unit_price": 5000}},
	})
	if r.status != http.StatusForbidden {
		t.Fatalf("no-charge without permission = %d, want 403: %s", r.status, r.body)
	}
}

func TestInvoices_NoChargeAllowedWithPermission(t *testing.T) {
	e := setup(t)
	agentID, _ := e.seedAgent(t)
	e.grantOverride(t, agentID, "invoices.create")
	e.grantOverride(t, agentID, "invoices.no_charge")
	token := agentReloginToken(t, e, agentID)

	r := e.do(t, "POST", "/api/v1/invoices", token, map[string]any{
		"mode": "no_charge", "due_date": "2026-09-01",
		"lines": []map[string]any{{"description": "Waived", "quantity": 1, "unit_price": 5000}},
	})
	if r.status != http.StatusCreated {
		t.Fatalf("no-charge with permission = %d: %s", r.status, r.body)
	}
	var v struct{ Total int64 }
	r.into(t, &v)
	if v.Total != 0 {
		t.Fatalf("no-charge invoice total = %d, want 0", v.Total)
	}
}

// --- gate item 5: renewal auto-invoices -------------------------------------

func TestInvoices_AutoGeneratedOnPricedRenewal(t *testing.T) {
	e := setup(t)
	prof := e.createProfile(t, 30000, 30)
	sub := e.createSubscriber(t, prof)

	r := e.do(t, "POST", "/api/v1/subscribers/"+sub+"/renew", e.token, map[string]any{})
	if r.status != http.StatusOK {
		t.Fatalf("renew = %d: %s", r.status, r.body)
	}
	var renewRes struct {
		LedgerTxID string `json:"ledger_tx_id"`
	}
	r.into(t, &renewRes)

	r = e.do(t, "GET", "/api/v1/invoices?subscriber_id="+sub, e.token, nil)
	if r.status != http.StatusOK {
		t.Fatalf("list invoices = %d: %s", r.status, r.body)
	}
	var list struct {
		Items []struct {
			ID     string `json:"id"`
			Total  int64  `json:"total"`
			Status string `json:"status"`
		} `json:"items"`
	}
	r.into(t, &list)
	if len(list.Items) != 1 {
		t.Fatalf("want exactly 1 auto-generated invoice, got %d", len(list.Items))
	}
	inv := list.Items[0]
	if inv.Total != 30000 || inv.Status != "paid" {
		t.Fatalf("auto-invoice = total=%d status=%q, want 30000/paid", inv.Total, inv.Status)
	}

	// The invoice's journal entry is the SAME balanced entry the renewal
	// itself posted (no double-counted revenue) — verified by checking a
	// journal entry exists tied to the renewal's own ledger_tx_id and has
	// at least 2 balanced lines.
	var entryID string
	if err := e.db.QueryRow(context.Background(),
		`SELECT id::text FROM journal_entries WHERE ledger_tx_id = $1::uuid ORDER BY at DESC LIMIT 1`, renewRes.LedgerTxID).
		Scan(&entryID); err != nil {
		t.Fatalf("no journal entry for renewal: %v", err)
	}
	if n := e.journalEntryLineCount(t, entryID); n < 2 {
		t.Fatalf("renewal journal entry has %d lines, want >= 2", n)
	}
}

// --- gate items 7/8: undo reverses / refuses correctly ----------------------

// TestUndo_ReversesRenewal_BothInAuditLog — undo restores the subscriber's
// exact pre-renewal expiry for every starting state: never-expires (NULL — a
// freshly created subscriber), a future expiry, and an already-lapsed one.
func TestUndo_ReversesRenewal_BothInAuditLog(t *testing.T) {
	e := setup(t)
	prof := e.createProfile(t, 15000, 30)
	cases := []struct {
		name, seedExpiry, wantStatus string
	}{
		{"never_expires", "", "active"},
		{"future_expiry", "now() + interval '10 days'", "active"},
		{"lapsed_expiry", "now() - interval '3 days'", "expired"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sub := e.createSubscriber(t, prof)
			if tc.seedExpiry != "" {
				if _, err := e.db.Exec(context.Background(),
					`UPDATE subscribers SET expires_at = `+tc.seedExpiry+` WHERE id=$1::uuid`, sub); err != nil {
					t.Fatal(err)
				}
			}
			e.undoRenewalRestoresExpiry(t, sub, tc.wantStatus)
		})
	}
}

func (e env) undoRenewalRestoresExpiry(t *testing.T, sub, wantStatus string) {
	t.Helper()
	var before *time.Time
	if err := e.db.QueryRow(context.Background(), `SELECT expires_at FROM subscribers WHERE id=$1::uuid`, sub).Scan(&before); err != nil {
		t.Fatal(err)
	}

	r := e.do(t, "POST", "/api/v1/subscribers/"+sub+"/renew", e.token, map[string]any{})
	if r.status != http.StatusOK {
		t.Fatalf("renew = %d: %s", r.status, r.body)
	}
	var renewRes struct {
		UndoID string `json:"undo_id"`
	}
	r.into(t, &renewRes)
	if renewRes.UndoID == "" {
		t.Fatal("renew response carried no undo_id")
	}

	r = e.do(t, "POST", "/api/v1/undo/"+renewRes.UndoID, e.token, nil)
	if r.status != http.StatusOK {
		t.Fatalf("undo = %d: %s", r.status, r.body)
	}

	var after *time.Time
	var status string
	if err := e.db.QueryRow(context.Background(), `SELECT expires_at, status FROM subscribers WHERE id=$1::uuid`, sub).Scan(&after, &status); err != nil {
		t.Fatal(err)
	}
	if (after == nil) != (before == nil) || (after != nil && !after.Equal(*before)) {
		t.Fatalf("expiry after undo = %v, want rolled back to %v", after, before)
	}
	if status != wantStatus {
		t.Fatalf("status after undo = %q, want %q", status, wantStatus)
	}

	var renewCount, undoCount int
	if err := e.db.QueryRow(context.Background(),
		`SELECT count(*) FROM audit_log WHERE entity_type='subscriber' AND entity_id=$1 AND action='subscriber.renew'`, sub).
		Scan(&renewCount); err != nil {
		t.Fatal(err)
	}
	if err := e.db.QueryRow(context.Background(),
		`SELECT count(*) FROM audit_log WHERE entity_type='undo_log' AND action='billing.undo'`).
		Scan(&undoCount); err != nil {
		t.Fatal(err)
	}
	if renewCount == 0 || undoCount == 0 {
		t.Fatalf("want both the original action and its reversal in the audit log: renew=%d undo=%d", renewCount, undoCount)
	}
}

func TestUndo_RefusesWhenWindowClosed(t *testing.T) {
	e := setup(t)
	prof := e.createProfile(t, 15000, 30)
	sub := e.createSubscriber(t, prof)
	r := e.do(t, "POST", "/api/v1/subscribers/"+sub+"/renew", e.token, map[string]any{})
	var renewRes struct {
		UndoID string `json:"undo_id"`
	}
	r.into(t, &renewRes)

	if _, err := e.db.Exec(context.Background(), `UPDATE undo_log SET expires_at = now() - interval '1 minute' WHERE id = $1::uuid`, renewRes.UndoID); err != nil {
		t.Fatal(err)
	}
	r = e.do(t, "POST", "/api/v1/undo/"+renewRes.UndoID, e.token, nil)
	if r.status != http.StatusConflict {
		t.Fatalf("undo after window closed = %d, want 409: %s", r.status, r.body)
	}
	var body struct {
		Error struct{ Code string } `json:"error"`
	}
	r.into(t, &body)
	if body.Error.Code != "window_closed" {
		t.Fatalf("error code = %q, want window_closed", body.Error.Code)
	}
}

func TestUndo_RefusesWhenAlreadyUndone(t *testing.T) {
	e := setup(t)
	prof := e.createProfile(t, 15000, 30)
	sub := e.createSubscriber(t, prof)
	r := e.do(t, "POST", "/api/v1/subscribers/"+sub+"/renew", e.token, map[string]any{})
	var renewRes struct {
		UndoID string `json:"undo_id"`
	}
	r.into(t, &renewRes)

	if r := e.do(t, "POST", "/api/v1/undo/"+renewRes.UndoID, e.token, nil); r.status != http.StatusOK {
		t.Fatalf("first undo = %d: %s", r.status, r.body)
	}
	r = e.do(t, "POST", "/api/v1/undo/"+renewRes.UndoID, e.token, nil)
	if r.status != http.StatusConflict {
		t.Fatalf("second undo = %d, want 409: %s", r.status, r.body)
	}
	var body struct {
		Error struct{ Code string } `json:"error"`
	}
	r.into(t, &body)
	if body.Error.Code != "already_undone" {
		t.Fatalf("error code = %q, want already_undone", body.Error.Code)
	}
}

func TestUndo_RefusesWhenRecordGone(t *testing.T) {
	e := setup(t)
	// A synthetic undo_log row referencing a manager id that does not
	// exist — the cleanest way to exercise "the referenced record no
	// longer exists" without depending on a real deletion path (topup
	// undo's own record-gone check reads the managers table directly).
	var fakeID string
	if err := e.db.QueryRow(context.Background(),
		`INSERT INTO undo_log (kind, entity_type, entity_id, actor_manager_id, snapshot, expires_at)
		 VALUES ('topup', 'manager', gen_random_uuid(), $1::uuid, $2, now() + interval '5 minutes')
		 RETURNING id::text`, e.adminID, `{"manager_id":"00000000-0000-0000-0000-000000000000","amount":1000}`).
		Scan(&fakeID); err != nil {
		t.Fatal(err)
	}
	r := e.do(t, "POST", "/api/v1/undo/"+fakeID, e.token, nil)
	if r.status != http.StatusConflict {
		t.Fatalf("undo of a gone record = %d, want 409: %s", r.status, r.body)
	}
	var body struct {
		Error struct{ Code string } `json:"error"`
	}
	r.into(t, &body)
	if body.Error.Code != "record_gone" {
		t.Fatalf("error code = %q, want record_gone", body.Error.Code)
	}
}

func TestUndo_RefusesWhenChangedSince(t *testing.T) {
	e := setup(t)
	prof := e.createProfile(t, 15000, 30)
	sub := e.createSubscriber(t, prof)
	r := e.do(t, "POST", "/api/v1/subscribers/"+sub+"/renew", e.token, map[string]any{})
	var first struct {
		UndoID string `json:"undo_id"`
	}
	r.into(t, &first)

	// A second renewal changes expires_at again before the first undo runs.
	if r := e.do(t, "POST", "/api/v1/subscribers/"+sub+"/renew", e.token, map[string]any{}); r.status != http.StatusOK {
		t.Fatalf("second renew = %d: %s", r.status, r.body)
	}

	r = e.do(t, "POST", "/api/v1/undo/"+first.UndoID, e.token, nil)
	if r.status != http.StatusConflict {
		t.Fatalf("undo of a stale renewal = %d, want 409: %s", r.status, r.body)
	}
	var body struct {
		Error struct{ Code string } `json:"error"`
	}
	r.into(t, &body)
	if body.Error.Code != "changed" {
		t.Fatalf("error code = %q, want changed", body.Error.Code)
	}
}

// --- gate items 9/10: approval gate splits correctly and replays one path --

func TestApprovals_ScopedUndoParksAndReplaysTheSamePath(t *testing.T) {
	e := setup(t)
	agentID, agentToken := e.seedAgent(t)
	prof := e.createProfile(t, 15000, 30)
	sub := e.createSubscriber(t, prof)
	// v4-12 (FR-175): same ownership fix as TestJournal_DualWriteAgrees_TopupRenewRefund
	// above — a scoped agent's renew 404s "subscriber not found" against an
	// unowned subscriber.
	if _, err := e.db.Exec(context.Background(),
		`UPDATE subscribers SET owner_manager_id=$1::uuid WHERE id=$2::uuid`, agentID, sub); err != nil {
		t.Fatalf("assign owner: %v", err)
	}
	e.do(t, "POST", "/api/v1/managers/"+agentID+"/topup", e.token, map[string]any{"amount": 100000})

	r := e.do(t, "POST", "/api/v1/subscribers/"+sub+"/renew", agentToken, map[string]any{})
	if r.status != http.StatusOK {
		t.Fatalf("agent renew = %d: %s", r.status, r.body)
	}
	var renewRes struct {
		UndoID string `json:"undo_id"`
	}
	r.into(t, &renewRes)

	balanceBeforeUndo := e.cachedBalance(t, agentID)

	// The agent (scoped) requests undo — must PARK, not execute.
	r = e.do(t, "POST", "/api/v1/undo/"+renewRes.UndoID, agentToken, nil)
	if r.status != http.StatusAccepted {
		t.Fatalf("scoped undo = %d, want 202 pending_approval: %s", r.status, r.body)
	}
	var pending struct {
		PendingApproval   bool   `json:"pending_approval"`
		ApprovalRequestID string `json:"approval_request_id"`
	}
	r.into(t, &pending)
	if !pending.PendingApproval || pending.ApprovalRequestID == "" {
		t.Fatalf("want a pending approval request, got %+v", pending)
	}
	if got := e.cachedBalance(t, agentID); got != balanceBeforeUndo {
		t.Fatalf("balance changed before approval: %d -> %d", balanceBeforeUndo, got)
	}

	// A manager holding approvals.manage approves it — this calls
	// performUndo, the EXACT SAME function the direct (unscoped) undo path
	// in undoHandler calls; gate item 10's proof is that this produces the
	// identical end state a direct call would (asserted below: balance
	// restored to the pre-renewal wallet amount, exactly as
	// TestUndo_ReversesRenewal_BothInAuditLog's direct-call path produces).
	r = e.do(t, "POST", "/api/v1/approval-requests/"+pending.ApprovalRequestID+"/approve", e.token, nil)
	if r.status != http.StatusOK {
		t.Fatalf("approve = %d: %s", r.status, r.body)
	}

	if got, want := e.cachedBalance(t, agentID), int64(100000); got != want {
		t.Fatalf("balance after approved undo = %d, want %d (fully restored)", got, want)
	}
	if bal, wallet := e.cachedBalance(t, agentID), e.walletJournalNet(t, agentID); bal != wallet {
		t.Fatalf("after approved undo: manager_balances=%d journal wallet net=%d, want equal", bal, wallet)
	}

	// A re-approve attempt 409s (not pending any more) — proves the
	// approval decision, not just the underlying action, is idempotently
	// guarded.
	r = e.do(t, "POST", "/api/v1/approval-requests/"+pending.ApprovalRequestID+"/approve", e.token, nil)
	if r.status != http.StatusConflict {
		t.Fatalf("re-approve = %d, want 409", r.status)
	}
}

func TestApprovals_UnscopedCallerExecutesImmediately(t *testing.T) {
	e := setup(t)
	prof := e.createProfile(t, 15000, 30)
	sub := e.createSubscriber(t, prof)
	r := e.do(t, "POST", "/api/v1/subscribers/"+sub+"/renew", e.token, map[string]any{})
	var renewRes struct {
		UndoID string `json:"undo_id"`
	}
	r.into(t, &renewRes)

	// The admin (unscoped) undoes directly — no approval request created.
	r = e.do(t, "POST", "/api/v1/undo/"+renewRes.UndoID, e.token, nil)
	if r.status != http.StatusOK {
		t.Fatalf("unscoped undo = %d, want 200 (executes immediately, no parking): %s", r.status, r.body)
	}
}

// --- gate item 11: credit limit and commission ------------------------------

func TestCreditLimitBlocksAgentAtTheConfiguredLimit(t *testing.T) {
	e := setup(t)
	agentID, agentToken := e.seedAgent(t)
	e.setCreditLimit(t, agentID, 5000)
	prof := e.createProfile(t, 10000, 30)
	sub := e.createSubscriber(t, prof)
	// v4-12 (FR-175): same ownership fix as TestJournal_DualWriteAgrees_TopupRenewRefund.
	if _, err := e.db.Exec(context.Background(),
		`UPDATE subscribers SET owner_manager_id=$1::uuid WHERE id=$2::uuid`, agentID, sub); err != nil {
		t.Fatalf("assign owner: %v", err)
	}

	// Balance 0, limit 5000: charging 10000 would take the balance to
	// -10000, past the -5000 floor — refused.
	r := e.do(t, "POST", "/api/v1/subscribers/"+sub+"/renew", agentToken, map[string]any{})
	if r.status != http.StatusUnprocessableEntity {
		t.Fatalf("renew past credit limit = %d, want 422 insufficient_balance: %s", r.status, r.body)
	}

	// Topping up to 6000 brings 6000-10000=-4000, within the -5000 floor —
	// allowed.
	e.do(t, "POST", "/api/v1/managers/"+agentID+"/topup", e.token, map[string]any{"amount": 6000})
	r = e.do(t, "POST", "/api/v1/subscribers/"+sub+"/renew", agentToken, map[string]any{})
	if r.status != http.StatusOK {
		t.Fatalf("renew within credit limit = %d: %s", r.status, r.body)
	}
}

// --- security review finding: invoice scope isolation ----------------------

// TestInvoices_ScopeIsolation_AgentCannotTouchAnotherAgentsInvoice proves the
// v4-9 security-review fix in invoices_api.go (invoiceVisible): a scoped
// agent may not view, void, or post a receipt against an invoice belonging
// to a subscriber they do not own — the same reseller-isolation discipline
// as gate item 12, applied to the write paths a naive implementation would
// have left globally reachable by anyone holding invoices.manage.
func TestInvoices_ScopeIsolation_AgentCannotTouchAnotherAgentsInvoice(t *testing.T) {
	e := setup(t)
	ownerAgentID, _ := e.seedAgent(t)
	otherAgentID, _ := e.seedAgent(t)
	// Hold every invoices.* verb the three routes require, so each request
	// clears its permission gate (which 403s regardless of the id) and the
	// scoping check under test (invoiceVisible → 404) is what answers.
	e.grantOverride(t, otherAgentID, "invoices.view")
	e.grantOverride(t, otherAgentID, "invoices.manage")
	e.grantOverride(t, otherAgentID, "invoices.create")
	otherToken := agentReloginToken(t, e, otherAgentID)

	prof := e.createProfile(t, 15000, 30)
	sub := e.createSubscriber(t, prof)
	if _, err := e.db.Exec(context.Background(),
		`UPDATE subscribers SET owner_manager_id = $2::uuid WHERE id = $1::uuid`, sub, ownerAgentID); err != nil {
		t.Fatal(err)
	}

	r := e.do(t, "POST", "/api/v1/invoices", e.token, map[string]any{
		"subscriber_id": sub, "mode": "full", "due_date": "2026-09-01",
		"lines": []map[string]any{{"description": "Plan", "quantity": 1, "unit_price": 15000}},
	})
	if r.status != http.StatusCreated {
		t.Fatalf("create invoice = %d: %s", r.status, r.body)
	}
	var inv struct{ ID string }
	r.into(t, &inv)

	// otherAgent (unrelated to sub's owner) cannot see, void, or pay it.
	if r := e.do(t, "GET", "/api/v1/invoices/"+inv.ID, otherToken, nil); r.status != http.StatusNotFound {
		t.Fatalf("cross-tenant get = %d, want 404: %s", r.status, r.body)
	}
	if r := e.do(t, "POST", "/api/v1/invoices/"+inv.ID+"/void", otherToken, map[string]any{"reason": "x"}); r.status != http.StatusNotFound {
		t.Fatalf("cross-tenant void = %d, want 404: %s", r.status, r.body)
	}
	if r := e.do(t, "POST", "/api/v1/invoices/"+inv.ID+"/receipts", otherToken, map[string]any{"amount": 1000}); r.status != http.StatusNotFound {
		t.Fatalf("cross-tenant receipt = %d, want 404: %s", r.status, r.body)
	}
}

func TestCommissionLandsAsExplicitJournalLines(t *testing.T) {
	e := setup(t)
	agentID, agentToken := e.seedAgent(t)
	e.setCommission(t, agentID, 10) // 10%
	prof := e.createProfile(t, 10000, 30)
	sub := e.createSubscriber(t, prof)
	// v4-12 (FR-175): same ownership fix as TestJournal_DualWriteAgrees_TopupRenewRefund.
	if _, err := e.db.Exec(context.Background(),
		`UPDATE subscribers SET owner_manager_id=$1::uuid WHERE id=$2::uuid`, agentID, sub); err != nil {
		t.Fatalf("assign owner: %v", err)
	}
	e.do(t, "POST", "/api/v1/managers/"+agentID+"/topup", e.token, map[string]any{"amount": 100000})

	r := e.do(t, "POST", "/api/v1/subscribers/"+sub+"/renew", agentToken, map[string]any{})
	if r.status != http.StatusOK {
		t.Fatalf("renew = %d: %s", r.status, r.body)
	}

	// 100000 topup - 10000 renewal + 1000 commission (10% of 10000) = 91000.
	if got, want := e.cachedBalance(t, agentID), int64(91000); got != want {
		t.Fatalf("balance after commission = %d, want %d", got, want)
	}
	if bal, wallet := e.cachedBalance(t, agentID), e.walletJournalNet(t, agentID); bal != wallet {
		t.Fatalf("manager_balances=%d journal wallet net=%d, want equal", bal, wallet)
	}

	var commissionLedgerCount int
	if err := e.db.QueryRow(context.Background(),
		`SELECT count(*) FROM ledger_transactions WHERE actor_manager_id=$1::uuid AND type='commission'`, agentID).
		Scan(&commissionLedgerCount); err != nil {
		t.Fatal(err)
	}
	if commissionLedgerCount != 1 {
		t.Fatalf("commission ledger rows = %d, want 1", commissionLedgerCount)
	}

	var commissionJournalLines int
	if err := e.db.QueryRow(context.Background(),
		`SELECT count(*) FROM journal_lines jl JOIN accounts a ON a.id=jl.account_id
		  WHERE a.code = '5100'`). // Commission Expense
		Scan(&commissionJournalLines); err != nil {
		t.Fatal(err)
	}
	if commissionJournalLines == 0 {
		t.Fatal("want at least one Commission Expense journal line")
	}
}
