package billing_test

// Gate items 2-5 (v2 phase 4): per-currency reconciliation, exchange, non-IQD
// renewal, and refund-reverses-original-currency. All run against the shared
// DB-gated env (setup(t), same as db_test.go).

import (
	"context"
	"net/http"
	"testing"
)

// createProfileCurrency is createProfile's sibling for a non-IQD plan.
func (e env) createProfileCurrency(t *testing.T, price int64, currency string, days int) string {
	t.Helper()
	r := e.do(t, "POST", "/api/v1/profiles", e.token, map[string]any{
		"name": uniq("plan_"), "price": price, "currency": currency, "duration_days": days,
		"rate_down_kbps": 10240, "rate_up_kbps": 2048,
	})
	if r.status != http.StatusCreated {
		t.Fatalf("create %s profile = %d: %s", currency, r.status, r.body)
	}
	var p struct {
		ID string `json:"id"`
	}
	r.into(t, &p)
	return p.ID
}

// balanceFor reads the raw ledger-derived balance for (manager, currency),
// scoped to that currency only — the per-currency analogue of cachedBalance.
func (e env) balanceFor(t *testing.T, mgr, currency string) int64 {
	t.Helper()
	var n int64
	if err := e.db.QueryRow(context.Background(),
		`SELECT COALESCE((SELECT balance FROM manager_balances WHERE manager_id=$1::uuid AND currency=$2),0)`,
		mgr, currency).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// ledgerSumFor is ledgerSum scoped to one currency.
func (e env) ledgerSumFor(t *testing.T, mgr, currency string) int64 {
	t.Helper()
	var n int64
	if err := e.db.QueryRow(context.Background(),
		`SELECT COALESCE(sum(amount),0) FROM ledger_transactions WHERE actor_manager_id=$1::uuid AND currency=$2`,
		mgr, currency).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func (e env) insertCurrencyRate(t *testing.T, from, to string, rate float64) string {
	t.Helper()
	r := e.do(t, "POST", "/api/v1/currency-rates", e.token,
		map[string]any{"from_currency": from, "to_currency": to, "rate": rate})
	if r.status != http.StatusCreated {
		t.Fatalf("create currency rate %s->%s = %d: %s", from, to, r.status, r.body)
	}
	var v struct {
		ID string `json:"id"`
	}
	r.into(t, &v)
	return v.ID
}

// TestPerCurrencyReconciliationInvariant — AC-69c: balance(M,C) = sum(ledger
// entries where actor=M and currency=C) holds independently for IQD and USD on
// the SAME manager. Mutation-checked (verified manually during this phase's
// build: temporarily dropping the "AND currency = $2" clause from
// recomputeBalance's WHERE in internal/billing/ledger.go made this test fail —
// see the phase gate-result.md for the exact before/after — so this is not
// passing vacuously).
func TestPerCurrencyReconciliationInvariant(t *testing.T) {
	e := setup(t)
	agentID, _ := e.seedAgent(t)

	e.do(t, "POST", "/api/v1/managers/"+agentID+"/topup", e.token,
		map[string]any{"amount": 100000, "currency": "IQD"})
	e.do(t, "POST", "/api/v1/managers/"+agentID+"/topup", e.token,
		map[string]any{"amount": 5000, "currency": "USD"})
	e.do(t, "POST", "/api/v1/managers/"+agentID+"/topup", e.token,
		map[string]any{"amount": 25000, "currency": "IQD"})

	if got, want := e.balanceFor(t, agentID, "IQD"), e.ledgerSumFor(t, agentID, "IQD"); got != want {
		t.Errorf("IQD: cached balance %d != ledger sum %d", got, want)
	}
	if got, want := e.balanceFor(t, agentID, "USD"), e.ledgerSumFor(t, agentID, "USD"); got != want {
		t.Errorf("USD: cached balance %d != ledger sum %d", got, want)
	}
	if e.balanceFor(t, agentID, "IQD") != 125000 {
		t.Errorf("IQD balance = %d, want 125000 (two IQD top-ups, USD top-up must not leak in)", e.balanceFor(t, agentID, "IQD"))
	}
	if e.balanceFor(t, agentID, "USD") != 5000 {
		t.Errorf("USD balance = %d, want 5000 (one USD top-up, IQD top-ups must not leak in)", e.balanceFor(t, agentID, "USD"))
	}
}

// TestExchangePairCorrectness — AC-69b: an exchange writes exactly two linked
// ledger rows with correct signs and currency_rate_id, the minor-unit-adjusted
// ToAmount, both balances move by exactly the exchanged amounts, and no other
// currency on the manager is touched.
func TestExchangePairCorrectness(t *testing.T) {
	e := setup(t)
	agentID, _ := e.seedAgent(t)
	e.do(t, "POST", "/api/v1/managers/"+agentID+"/topup", e.token,
		map[string]any{"amount": 100000, "currency": "IQD"})
	// A EUR balance that must stay untouched by an IQD->USD exchange.
	e.do(t, "POST", "/api/v1/managers/"+agentID+"/topup", e.token,
		map[string]any{"amount": 1000, "currency": "EUR"})

	rateID := e.insertCurrencyRate(t, "IQD", "USD", 0.00075)

	r := e.do(t, "POST", "/api/v1/managers/"+agentID+"/exchange", e.token, map[string]any{
		"from_currency": "IQD", "to_currency": "USD", "amount": 100000, "currency_rate_id": rateID,
	})
	if r.status != http.StatusOK {
		t.Fatalf("exchange = %d: %s", r.status, r.body)
	}
	var out struct {
		ExchangeReference string `json:"exchange_reference"`
		FromLedgerTxID    string `json:"from_ledger_tx_id"`
		ToLedgerTxID      string `json:"to_ledger_tx_id"`
		FromBalance       int64  `json:"from_balance"`
		ToBalance         int64  `json:"to_balance"`
	}
	r.into(t, &out)

	// 100000 IQD (0 minor-unit digits) * 0.00075 = 75.00 USD = 7500 minor units.
	if out.ToBalance != 7500 {
		t.Errorf("to_balance = %d, want 7500 (75.00 USD)", out.ToBalance)
	}
	if out.FromBalance != 0 {
		t.Errorf("from_balance = %d, want 0", out.FromBalance)
	}

	var n int
	if err := e.db.QueryRow(context.Background(),
		`SELECT count(*) FROM ledger_transactions WHERE reference=$1 AND type='exchange'`,
		out.ExchangeReference).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("exchange produced %d ledger rows sharing the reference, want exactly 2", n)
	}

	var fromAmt, toAmt int64
	var fromCur, toCur string
	var fromRateID, toRateID string
	if err := e.db.QueryRow(context.Background(),
		`SELECT amount, currency, currency_rate_id::text FROM ledger_transactions WHERE id=$1::uuid`,
		out.FromLedgerTxID).Scan(&fromAmt, &fromCur, &fromRateID); err != nil {
		t.Fatal(err)
	}
	if err := e.db.QueryRow(context.Background(),
		`SELECT amount, currency, currency_rate_id::text FROM ledger_transactions WHERE id=$1::uuid`,
		out.ToLedgerTxID).Scan(&toAmt, &toCur, &toRateID); err != nil {
		t.Fatal(err)
	}
	if fromAmt != -100000 || fromCur != "IQD" || fromRateID != rateID {
		t.Errorf("from leg = (%d, %q, rate=%s), want (-100000, IQD, rate=%s)", fromAmt, fromCur, fromRateID, rateID)
	}
	if toAmt != 7500 || toCur != "USD" || toRateID != rateID {
		t.Errorf("to leg = (%d, %q, rate=%s), want (7500, USD, rate=%s)", toAmt, toCur, toRateID, rateID)
	}

	// The EUR balance (a currency untouched by this exchange) is unaffected.
	if e.balanceFor(t, agentID, "EUR") != 1000 {
		t.Errorf("EUR balance = %d, want 1000 (untouched by an IQD->USD exchange)", e.balanceFor(t, agentID, "EUR"))
	}
}

// TestNonIQDRenewalDebitsOnlyThatCurrency — AC-69a: a USD-priced profile
// renewal debits only the agent's USD balance; an independently-seeded
// non-zero IQD balance is provably untouched; the payment row records
// currency='USD'.
func TestNonIQDRenewalDebitsOnlyThatCurrency(t *testing.T) {
	e := setup(t)
	prof := e.createProfileCurrency(t, 2500, "USD", 30) // $25.00
	sub := e.createSubscriber(t, prof)
	agentID, agentTok := e.seedAgent(t)
	_, _ = e.db.Exec(context.Background(),
		`UPDATE subscribers SET owner_manager_id=$1::uuid WHERE id=$2::uuid`, agentID, sub)

	e.do(t, "POST", "/api/v1/managers/"+agentID+"/topup", e.token,
		map[string]any{"amount": 5000, "currency": "USD"}) // $50.00
	e.do(t, "POST", "/api/v1/managers/"+agentID+"/topup", e.token,
		map[string]any{"amount": 100000, "currency": "IQD"})

	r := e.do(t, "POST", "/api/v1/subscribers/"+sub+"/renew", agentTok, map[string]any{})
	if r.status != http.StatusOK {
		t.Fatalf("renew = %d: %s", r.status, r.body)
	}
	var out struct {
		ReceiptNo string `json:"receipt_no"`
		Currency  string `json:"currency"`
	}
	r.into(t, &out)
	if out.Currency != "USD" {
		t.Errorf("renew response currency = %q, want USD", out.Currency)
	}

	if got := e.balanceFor(t, agentID, "USD"); got != 2500 {
		t.Errorf("USD balance = %d, want 2500 ($50.00 - $25.00)", got)
	}
	if got := e.balanceFor(t, agentID, "IQD"); got != 100000 {
		t.Errorf("IQD balance = %d, want 100000 — untouched by a USD renewal", got)
	}

	var payAmount int64
	var payCur string
	if err := e.db.QueryRow(context.Background(),
		`SELECT amount, currency FROM payments WHERE receipt_no=$1`, out.ReceiptNo).
		Scan(&payAmount, &payCur); err != nil {
		t.Fatal(err)
	}
	if payAmount != 2500 || payCur != "USD" {
		t.Errorf("payment = (%d, %q), want (2500, USD)", payAmount, payCur)
	}
}

// TestRefundReversesOriginalCurrencyNoReResolution — AC-69d: a USD renewal's
// refund is a USD reversing entry at the ORIGINAL amount, even after the
// profile's price and currency have since changed. Proves refund.go reads
// l.currency from the reversed row itself rather than re-resolving from
// today's profile.
func TestRefundReversesOriginalCurrencyNoReResolution(t *testing.T) {
	e := setup(t)
	prof := e.createProfileCurrency(t, 2500, "USD", 30)
	sub := e.createSubscriber(t, prof)
	agentID, agentTok := e.seedAgent(t)
	_, _ = e.db.Exec(context.Background(),
		`UPDATE subscribers SET owner_manager_id=$1::uuid WHERE id=$2::uuid`, agentID, sub)
	e.do(t, "POST", "/api/v1/managers/"+agentID+"/topup", e.token,
		map[string]any{"amount": 5000, "currency": "USD"})

	r := e.do(t, "POST", "/api/v1/subscribers/"+sub+"/renew", agentTok, map[string]any{})
	if r.status != http.StatusOK {
		t.Fatalf("renew = %d: %s", r.status, r.body)
	}
	var out struct {
		LedgerTxID string `json:"ledger_tx_id"`
	}
	r.into(t, &out)

	// The profile's price AND currency both change after the renewal — the
	// refund must never re-resolve against this.
	if _, err := e.db.Exec(context.Background(),
		`UPDATE profiles SET price=99999, currency='EUR' WHERE id=$1::uuid`, prof); err != nil {
		t.Fatal(err)
	}

	rf := e.do(t, "POST", "/api/v1/subscribers/"+sub+"/refund", e.token,
		map[string]any{"ledger_tx_id": out.LedgerTxID, "reason": "AC-69d proof"})
	if rf.status != http.StatusOK {
		t.Fatalf("refund = %d: %s", rf.status, rf.body)
	}

	// Balance restored to exactly the original USD top-up — not touched by the
	// profile's now-EUR currency or its new price.
	if got := e.balanceFor(t, agentID, "USD"); got != 5000 {
		t.Errorf("USD balance after refund = %d, want 5000 (fully restored)", got)
	}
	if got := e.balanceFor(t, agentID, "EUR"); got != 0 {
		t.Errorf("EUR balance = %d, want 0 — the refund must never touch EUR", got)
	}

	var reversalAmt int64
	var reversalCur string
	if err := e.db.QueryRow(context.Background(),
		`SELECT amount, currency FROM ledger_transactions WHERE reverses_id=$1::uuid`, out.LedgerTxID).
		Scan(&reversalAmt, &reversalCur); err != nil {
		t.Fatal(err)
	}
	if reversalAmt != 2500 || reversalCur != "USD" {
		t.Errorf("reversal = (%d, %q), want (2500, USD) — the original amount/currency, not the profile's current EUR", reversalAmt, reversalCur)
	}
}
