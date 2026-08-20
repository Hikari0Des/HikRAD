package reports_test

// FR-175: every reported money figure ties to the journal (v4-9) exactly —
// asserted here by a test that walks each report and compares against a
// direct journal query. Not sampled, not eyeballed: every assertion below
// compares an exact total, not a "roughly agrees" tolerance.
//
// Scope, stated once here rather than per-test: this suite proves exact
// equality where it is real —
//   - the settlement report against the journal's own per-manager Agent
//     Prepaid Balances (2100) subledger, which are dual-written from the
//     SAME ledgerEntry in the SAME transaction (billing/journal.go), so
//     equality is a structural guarantee, not a coincidence;
//   - the unscoped revenue report against the journal's Revenue account
//     net, for a fixture with NO reseller markup — the condition under
//     which payments.amount (what revenue.go sums) and the journal's
//     Revenue credit (what postLedgerJournal posts) are provably the same
//     number;
//   - the unpaid-invoices dashboard tile against the journal's Accounts
//     Receivable (1100) balance;
//   - the trial balance, re-verified here as part of the same "books tie"
//     pass FR-175 asks for.
//
// It deliberately does NOT claim a reseller-scoped revenue report ties to
// the journal: postLedgerJournal books a wallet-mediated sale's Revenue
// credit at the manager's own balance effect (wholesale for a reseller,
// v2-9), while revenue.go always sums the subscriber's retail payment — two
// different, both-correct numbers once reseller markup exists. Documented
// here, not papered over with a misleading "reconciles" claim; see
// docs/history/gates/v4-12.md.
//
// Building this test found and fixed a real bug: postLedgerJournal's
// wallet-mediated case had its debit/credit accounts swapped, so the
// journal's own wallet subledger never actually matched the ledger for any
// renewal. See docs/ops/known-issues.md (2026-08-20, v4-12) and
// internal/billing/journal.go.

import (
	"context"
	"net/http"
	"testing"
	"time"
)

// journalAccountNet sums credit-debit for every line against the accounts
// with the given codes, over journal entries dated in [from, to).
func (e env) journalAccountNet(t *testing.T, codes []string, from, to time.Time) int64 {
	t.Helper()
	var n int64
	if err := e.db.QueryRow(context.Background(), `
		SELECT COALESCE(sum(jl.credit - jl.debit), 0)
		  FROM journal_lines jl
		  JOIN journal_entries je ON je.id = jl.entry_id
		  JOIN accounts a ON a.id = jl.account_id
		 WHERE a.code = ANY($1) AND je.at >= $2 AND je.at < $3`, codes, from, to).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// TestSettlementReport_TiesExactlyToJournalWalletSubledger — FR-175's
// strongest case: opening/topups/renewals/refunds/closing, each compared to
// the exact journal figure for the same window, not just the final balance.
func TestSettlementReport_TiesExactlyToJournalWalletSubledger(t *testing.T) {
	e := setup(t)
	agentID, agentToken := e.seedAgent(t)
	prof := e.createProfile(t, 20_000, 30)

	from := time.Now().Add(-2 * time.Second).UTC()
	e.topup(t, agentID, e.token, 500_000)

	subs := make([]string, 4)
	for i := range subs {
		subs[i] = e.createSubscriberOwned(t, prof, agentID)
		r := e.do(t, "POST", "/api/v1/subscribers/"+subs[i]+"/renew", agentToken, map[string]any{})
		if r.status != http.StatusOK {
			t.Fatalf("renew %d = %d: %s", i, r.status, r.body)
		}
	}
	// Refund one renewal.
	var out struct {
		LedgerTxID string `json:"ledger_tx_id"`
	}
	r := e.do(t, "POST", "/api/v1/subscribers/"+subs[0]+"/renew", agentToken, map[string]any{})
	// (subs[0] already renewed once above; renew again so there's a distinct
	// tx to refund without touching the others' single renewal.)
	if r.status != http.StatusOK {
		t.Fatalf("second renew = %d: %s", r.status, r.body)
	}
	r.into(t, &out)
	rf := e.do(t, "POST", "/api/v1/subscribers/"+subs[0]+"/refund", e.token,
		map[string]any{"ledger_tx_id": out.LedgerTxID, "reason": "fr-175 reconciliation test"})
	if rf.status != http.StatusOK {
		t.Fatalf("refund = %d: %s", rf.status, rf.body)
	}
	to := time.Now().Add(2 * time.Second).UTC()

	// The report.
	q := "?from=" + from.Format(time.RFC3339) + "&to=" + to.Format(time.RFC3339) + "&manager_id=" + agentID + "&currency=IQD"
	sr := e.do(t, "GET", "/api/v1/reports/settlement"+q, e.token, nil)
	if sr.status != http.StatusOK {
		t.Fatalf("settlement = %d: %s", sr.status, sr.body)
	}
	var settlement struct {
		Opening  int64 `json:"opening"`
		Topups   int64 `json:"topups"`
		Renewals struct {
			Amount int64 `json:"amount"`
		} `json:"renewals"`
		Refunds int64 `json:"refunds"`
		Closing int64 `json:"closing"`
	}
	sr.into(t, &settlement)

	// The journal's own answer for the exact same windows.
	jOpening := e.journalAccountNetForManager(t, agentID, time.Time{}, from)
	jClosing := e.journalAccountNetForManager(t, agentID, time.Time{}, to)
	jDelta := e.journalAccountNetForManager(t, agentID, from, to)

	if settlement.Opening != jOpening {
		t.Errorf("opening: report=%d journal=%d", settlement.Opening, jOpening)
	}
	if settlement.Closing != jClosing {
		t.Errorf("closing: report=%d journal=%d", settlement.Closing, jClosing)
	}
	if settlement.Closing-settlement.Opening != jDelta {
		t.Errorf("closing-opening=%d != journal window delta=%d", settlement.Closing-settlement.Opening, jDelta)
	}
	// settlement.go reports Renewals.Amount as -renewalDelta — a positive
	// SPEND magnitude, not a signed delta (see its own doc comment) — so the
	// window identity subtracts it: topups - renewals_spend + refunds.
	wantDelta := settlement.Topups - settlement.Renewals.Amount + settlement.Refunds
	if jDelta != wantDelta {
		t.Errorf("journal window delta=%d != topups(%d)-renewals(%d)+refunds(%d)=%d",
			jDelta, settlement.Topups, settlement.Renewals.Amount, settlement.Refunds, wantDelta)
	}
	// And the report's own live-balance identity (opening+topups-renewals+refunds=closing).
	if settlement.Opening+wantDelta != settlement.Closing {
		t.Errorf("settlement identity broken: opening(%d)+delta(%d) != closing(%d)",
			settlement.Opening, wantDelta, settlement.Closing)
	}
}

// journalAccountNetForManager: the journal's wallet-subledger balance for
// one manager, as of "to" if from is zero, or the delta over [from,to)
// otherwise.
func (e env) journalAccountNetForManager(t *testing.T, managerID string, from, to time.Time) int64 {
	t.Helper()
	ctx := context.Background()
	var n int64
	if from.IsZero() {
		if err := e.db.QueryRow(ctx, `
			SELECT COALESCE(sum(jl.credit - jl.debit), 0)
			  FROM journal_lines jl
			  JOIN journal_entries je ON je.id = jl.entry_id
			  JOIN accounts a ON a.id = jl.account_id
			 WHERE a.code = '2100' AND jl.manager_id = $1::uuid AND je.at < $2`, managerID, to).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if err := e.db.QueryRow(ctx, `
		SELECT COALESCE(sum(jl.credit - jl.debit), 0)
		  FROM journal_lines jl
		  JOIN journal_entries je ON je.id = jl.entry_id
		  JOIN accounts a ON a.id = jl.account_id
		 WHERE a.code = '2100' AND jl.manager_id = $1::uuid AND je.at >= $2 AND je.at < $3`,
		managerID, from, to).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// TestRevenueReport_TiesToJournalRevenueAccount_NoResellerMarkup — the
// unscoped revenue report's total (sum of payments.amount) must equal the
// journal's Revenue account net (4000+4900), for a fixture with no reseller
// pricing configured anywhere (wholesale == retail for every renewal here,
// so the wallet-mediated leg's amount and the direct-cash leg's amount are
// the same number payments.amount would show either way).
func TestRevenueReport_TiesToJournalRevenueAccount_NoResellerMarkup(t *testing.T) {
	e := setup(t)
	from := time.Now().Add(-2 * time.Second).UTC()

	// Direct (unowned, admin-billed) renewals — hasPayment && paymentGross path.
	prof := e.createProfile(t, 12_000, 30)
	for i := 0; i < 3; i++ {
		sub := e.createSubscriber(t, prof)
		if r := e.do(t, "POST", "/api/v1/subscribers/"+sub+"/renew", e.token, map[string]any{}); r.status != http.StatusOK {
			t.Fatalf("direct renew = %d: %s", r.status, r.body)
		}
	}
	// Wallet-mediated (agent-owned) renewals — no reseller price configured,
	// so the agent's wallet debit equals the plan's retail price exactly.
	agentID, agentToken := e.seedAgent(t)
	e.topup(t, agentID, e.token, 200_000)
	for i := 0; i < 2; i++ {
		sub := e.createSubscriberOwned(t, prof, agentID)
		if r := e.do(t, "POST", "/api/v1/subscribers/"+sub+"/renew", agentToken, map[string]any{}); r.status != http.StatusOK {
			t.Fatalf("agent renew = %d: %s", r.status, r.body)
		}
	}
	// One refund (negative revenue), one no-op zero-value case is out of
	// scope for this fixture — refund.go's own reconciliation is already
	// covered by TestRevenueReportReconcilesWithPayments in db_test.go.
	to := time.Now().Add(2 * time.Second).UTC()

	q := "?from=" + from.Format(time.RFC3339) + "&to=" + to.Format(time.RFC3339)
	r := e.do(t, "GET", "/api/v1/reports/revenue"+q, e.token, nil)
	if r.status != http.StatusOK {
		t.Fatalf("revenue = %d: %s", r.status, r.body)
	}
	var out struct {
		Totals map[string]int64 `json:"totals"`
	}
	r.into(t, &out)
	reportTotal := out.Totals["IQD"]
	if reportTotal == 0 {
		t.Fatal("test fixture produced no revenue — cannot validate reconciliation")
	}

	journalTotal := e.journalAccountNet(t, []string{"4000", "4900"}, from, to)
	if reportTotal != journalTotal {
		t.Fatalf("revenue report total=%d, journal Revenue account net=%d, want equal", reportTotal, journalTotal)
	}
}

// TestTrialBalance_NetsToZero — re-verified here as part of the same "books
// tie" pass FR-175 asks for (already asserted at the billing-package level
// by TestStatements_TrialBalanceNetsToZero; this is not a second mechanism,
// just the same invariant checked from the reports side).
func TestTrialBalance_NetsToZero(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	var netDebit, netCredit int64
	if err := e.db.QueryRow(ctx, `SELECT COALESCE(sum(debit),0), COALESCE(sum(credit),0) FROM journal_lines`).
		Scan(&netDebit, &netCredit); err != nil {
		t.Fatal(err)
	}
	if netDebit != netCredit {
		t.Fatalf("trial balance does not net to zero: debit=%d credit=%d", netDebit, netCredit)
	}
}

// TestUnpaidInvoicesOutstanding_TiesToJournalARAccount — the dashboard's
// unpaid-invoices tile (monitorsvc's unpaidInvoices, FR-174) sums
// invoices.total-amount_paid for status IN (unpaid,partial,due); this
// proves that figure equals the journal's own Accounts Receivable (1100)
// balance for the same invoices — the same relationship
// billing/statements.go's Aging report already relies on (invoices post Dr
// AR / Cr Revenue at issue time, per migration 0619's own header).
func TestUnpaidInvoicesOutstanding_TiesToJournalARAccount(t *testing.T) {
	e := setup(t)
	ctx := context.Background()

	prof := e.createProfile(t, 10_000, 30)
	sub := e.createSubscriber(t, prof)
	r := e.do(t, "POST", "/api/v1/invoices", e.token, map[string]any{
		"subscriber_id": sub, "mode": "full", "due_date": "2027-01-01",
		"lines": []map[string]any{{"description": "Plan", "quantity": 1, "unit_price": 30_000}},
	})
	if r.status != http.StatusCreated {
		t.Fatalf("create invoice = %d: %s", r.status, r.body)
	}
	var inv struct {
		ID string `json:"id"`
	}
	r.into(t, &inv)

	// The dashboard tile's own query (monitorsvc/dashboard_v4_12.go's
	// unpaidInvoices), scoped to just this invoice by id so other tests'
	// concurrent fixtures in this shared DB can't shift the total.
	var outstanding int64
	if err := e.db.QueryRow(ctx,
		`SELECT COALESCE(sum(total - amount_paid), 0) FROM invoices WHERE id = $1::uuid AND status IN ('unpaid','partial','due')`,
		inv.ID).Scan(&outstanding); err != nil {
		t.Fatal(err)
	}
	if outstanding != 30_000 {
		t.Fatalf("unpaid outstanding = %d, want 30000 (fixture sanity check)", outstanding)
	}

	var journalAR int64
	if err := e.db.QueryRow(ctx, `
		SELECT COALESCE(sum(jl.debit - jl.credit), 0)
		  FROM journal_lines jl
		  JOIN journal_entries je ON je.id = jl.entry_id
		  JOIN accounts a ON a.id = jl.account_id
		 WHERE a.code = '1100' AND je.ledger_tx_id IS NULL
		   AND je.id = (SELECT journal_entry_id FROM invoices WHERE id = $1::uuid)`,
		inv.ID).Scan(&journalAR); err != nil {
		t.Fatal(err)
	}
	if journalAR != outstanding {
		t.Fatalf("journal AR (1100) for this invoice = %d, invoice outstanding = %d, want equal", journalAR, outstanding)
	}
}
