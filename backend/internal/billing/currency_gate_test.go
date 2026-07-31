package billing_test

// v3-2 single-book gate (FR-100/101/102). DELIBERATELY REPLACES the v2-4
// per-currency / exchange gate that lived here (Decision 54 supersedes FR-69.2/
// 69.3 clause by clause — see docs/v3/phases/phase-v3-2-currency-single-book/):
// the money model is one IQD book again, so the old per-currency reconciliation,
// exchange-pair, and non-IQD-renewal tests no longer describe the system. The
// new invariants: entry-time conversion is stamped, the book stays single-IQD,
// and the exchange endpoint is gone. DB-gated (setup(t), same as db_test.go).

import (
	"context"
	"net/http"
	"testing"
)

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

// stampFor reads the conversion stamp off a manager's most recent topup row.
func (e env) lastTopupStamp(t *testing.T, mgr string) (amount int64, entered *int64, enteredCur *string, rateID *string) {
	t.Helper()
	if err := e.db.QueryRow(context.Background(),
		`SELECT amount, entered_amount, entered_currency, currency_rate_id::text
		   FROM ledger_transactions
		  WHERE actor_manager_id=$1::uuid AND type='topup'
		  ORDER BY at DESC LIMIT 1`, mgr).Scan(&amount, &entered, &enteredCur, &rateID); err != nil {
		t.Fatal(err)
	}
	return
}

// TestEntryTimeConversionStamp — FR-101.1: a USD top-up converts to IQD at the
// current rate (round-half-up), lands as ONE IQD ledger row carrying the
// original figure + rate, and the single IQD book reconciles (cache == ledger).
func TestEntryTimeConversionStamp(t *testing.T) {
	e := setup(t)
	_, agentID := e.seedAgent(t)
	rateID := e.insertCurrencyRate(t, "USD", "IQD", 1310)

	// 5000 USD minor units = $50.00 → 50 * 1310 = 65,500 IQD.
	r := e.do(t, "POST", "/api/v1/managers/"+agentID+"/topup", e.token,
		map[string]any{"amount": 5000, "currency": "USD"})
	if r.status != http.StatusOK {
		t.Fatalf("USD topup = %d: %s", r.status, r.body)
	}

	if got := e.cachedBalance(t, agentID); got != 65500 {
		t.Fatalf("IQD balance = %d, want 65500", got)
	}
	if e.cachedBalance(t, agentID) != e.ledgerSum(t, agentID) {
		t.Fatalf("single-book cache %d != ledger %d", e.cachedBalance(t, agentID), e.ledgerSum(t, agentID))
	}

	amount, entered, enteredCur, gotRateID := e.lastTopupStamp(t, agentID)
	if amount != 65500 {
		t.Fatalf("row amount = %d, want 65500 (IQD)", amount)
	}
	if entered == nil || *entered != 5000 {
		t.Fatalf("entered_amount = %v, want 5000", entered)
	}
	if enteredCur == nil || *enteredCur != "USD" {
		t.Fatalf("entered_currency = %v, want USD", enteredCur)
	}
	if gotRateID == nil || *gotRateID != rateID {
		t.Fatalf("currency_rate_id = %v, want %s", gotRateID, rateID)
	}
}

// TestNativeIqdTopupHasNoStamp — a native IQD top-up is unchanged: IQD amount,
// no conversion stamp.
func TestNativeIqdTopupHasNoStamp(t *testing.T) {
	e := setup(t)
	_, agentID := e.seedAgent(t)
	r := e.do(t, "POST", "/api/v1/managers/"+agentID+"/topup", e.token,
		map[string]any{"amount": 25000, "currency": "IQD"})
	if r.status != http.StatusOK {
		t.Fatalf("IQD topup = %d: %s", r.status, r.body)
	}
	amount, entered, enteredCur, _ := e.lastTopupStamp(t, agentID)
	if amount != 25000 || entered != nil || enteredCur != nil {
		t.Fatalf("native IQD topup: amount=%d entered=%v cur=%v (want 25000/nil/nil)", amount, entered, enteredCur)
	}
}

// TestTopupUnknownRateRefused — FR-101.1: a foreign top-up with no defined rate
// is refused (422 no_rate), never silently converted 1:1.
func TestTopupUnknownRateRefused(t *testing.T) {
	e := setup(t)
	_, agentID := e.seedAgent(t)
	r := e.do(t, "POST", "/api/v1/managers/"+agentID+"/topup", e.token,
		map[string]any{"amount": 5000, "currency": "EUR"})
	if r.status != http.StatusUnprocessableEntity {
		t.Fatalf("EUR topup with no rate = %d, want 422: %s", r.status, r.body)
	}
	if e.cachedBalance(t, agentID) != 0 {
		t.Fatalf("balance moved despite refused conversion: %d", e.cachedBalance(t, agentID))
	}
}

// TestExchangeEndpointRetired — FR-100.3: the wallet-to-wallet exchange endpoint
// is gone (nothing to exchange between in a single IQD book).
func TestExchangeEndpointRetired(t *testing.T) {
	e := setup(t)
	_, agentID := e.seedAgent(t)
	r := e.do(t, "POST", "/api/v1/managers/"+agentID+"/exchange", e.token,
		map[string]any{"from_currency": "IQD", "to_currency": "USD", "amount": 1000})
	if r.status != http.StatusNotFound && r.status != http.StatusMethodNotAllowed {
		t.Fatalf("exchange endpoint = %d, want 404/405 (retired)", r.status)
	}
}
