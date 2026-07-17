package billing_test

// Gate items for v2 phase 9 (FR-71/74/76): cost stamping, reseller wholesale
// resolution, and independence from the subscriber's own retail charge. All
// run against the shared DB-gated env (setup(t), same as db_test.go).

import (
	"context"
	"net/http"
	"testing"
)

func (e env) setProfileCost(t *testing.T, profileID string, cost int64, currency string) {
	t.Helper()
	r := e.do(t, "POST", "/api/v1/profiles/"+profileID+"/cost", e.token,
		map[string]any{"cost": cost, "currency": currency})
	if r.status != http.StatusCreated {
		t.Fatalf("set profile cost = %d: %s", r.status, r.body)
	}
}

func (e env) setResellerPrice(t *testing.T, managerID, profileID string, subscriberID *string, price int64, currency string) {
	t.Helper()
	body := map[string]any{"manager_id": managerID, "profile_id": profileID, "price": price, "currency": currency}
	if subscriberID != nil {
		body["subscriber_id"] = *subscriberID
	}
	r := e.do(t, "POST", "/api/v1/reseller-prices", e.token, body)
	if r.status != http.StatusCreated {
		t.Fatalf("set reseller price = %d: %s", r.status, r.body)
	}
}

func (e env) costAtSale(t *testing.T, ledgerTxID string) *int64 {
	t.Helper()
	var v *int64
	if err := e.db.QueryRow(context.Background(),
		`SELECT cost_at_sale FROM ledger_transactions WHERE id=$1::uuid`, ledgerTxID).Scan(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

// TestUnknownCostStampsNilNeverZero — AC-71a (half): a profile with no cost
// ever recorded stamps a NULL cost_at_sale on its renewal ledger row, never
// a 0 (which would silently claim 100% margin).
func TestUnknownCostStampsNilNeverZero(t *testing.T) {
	e := setup(t)
	prof := e.createProfile(t, 25000, 30)
	sub := e.createSubscriber(t, prof)

	r := e.do(t, "POST", "/api/v1/subscribers/"+sub+"/renew", e.token, map[string]any{})
	if r.status != http.StatusOK {
		t.Fatalf("renew = %d: %s", r.status, r.body)
	}
	var out struct {
		LedgerTxID string `json:"ledger_tx_id"`
	}
	r.into(t, &out)

	if got := e.costAtSale(t, out.LedgerTxID); got != nil {
		t.Errorf("cost_at_sale = %v, want nil (no cost recorded)", *got)
	}
}

// TestKnownCostStampsCorrectly — AC-71a (half): a recorded cost is stamped
// exactly onto the renewal's ledger row, and a LATER cost change never
// rewrites that already-committed stamp (re-querying the same row later
// still returns the value that was in force at renewal time).
func TestKnownCostStampsCorrectly(t *testing.T) {
	e := setup(t)
	prof := e.createProfile(t, 25000, 30)
	sub := e.createSubscriber(t, prof)
	e.setProfileCost(t, prof, 10000, "IQD")

	r := e.do(t, "POST", "/api/v1/subscribers/"+sub+"/renew", e.token, map[string]any{})
	if r.status != http.StatusOK {
		t.Fatalf("renew = %d: %s", r.status, r.body)
	}
	var out struct {
		LedgerTxID string `json:"ledger_tx_id"`
	}
	r.into(t, &out)

	got := e.costAtSale(t, out.LedgerTxID)
	if got == nil || *got != 10000 {
		t.Fatalf("cost_at_sale = %v, want 10000", got)
	}

	// Re-pricing the plan afterward must not retroactively change what was
	// already stamped on this past renewal.
	e.setProfileCost(t, prof, 99999, "IQD")
	if got2 := e.costAtSale(t, out.LedgerTxID); got2 == nil || *got2 != 10000 {
		t.Errorf("cost_at_sale after re-pricing = %v, want unchanged 10000", got2)
	}
}

// TestRetailUnaffectedByNoResellerPrice — AC-74a: a reseller with no
// reseller_prices row debits exactly the retail price — byte-identical to
// pre-v2-9 behavior.
func TestRetailUnaffectedByNoResellerPrice(t *testing.T) {
	e := setup(t)
	prof := e.createProfile(t, 25000, 30)
	sub := e.createSubscriber(t, prof)
	agentID, agentTok := e.seedAgent(t)
	_, _ = e.db.Exec(context.Background(),
		`UPDATE subscribers SET owner_manager_id=$1::uuid WHERE id=$2::uuid`, agentID, sub)
	e.do(t, "POST", "/api/v1/managers/"+agentID+"/topup", e.token, map[string]any{"amount": 25000, "currency": "IQD"})

	r := e.do(t, "POST", "/api/v1/subscribers/"+sub+"/renew", agentTok, map[string]any{})
	if r.status != http.StatusOK {
		t.Fatalf("renew = %d: %s", r.status, r.body)
	}
	if got := e.balanceFor(t, agentID, "IQD"); got != 0 {
		t.Errorf("balance = %d, want 0 (debited full retail 25000)", got)
	}
}

// TestPerSubscriberOverrideBeatsPlanWide — AC-74b: a reseller with both a
// plan-wide and a per-subscriber wholesale price debits the per-subscriber
// price for that one subscriber and the plan-wide price for every other
// subscriber on the same plan; the subscribers themselves are always charged
// retail in both cases, unaffected by either wholesale number.
func TestPerSubscriberOverrideBeatsPlanWide(t *testing.T) {
	e := setup(t)
	prof := e.createProfile(t, 25000, 30) // retail 25000
	subVIP := e.createSubscriber(t, prof)
	subOther := e.createSubscriber(t, prof)
	agentID, agentTok := e.seedAgent(t)
	_, _ = e.db.Exec(context.Background(),
		`UPDATE subscribers SET owner_manager_id=$1::uuid WHERE id IN ($2::uuid, $3::uuid)`, agentID, subVIP, subOther)
	e.do(t, "POST", "/api/v1/managers/"+agentID+"/topup", e.token, map[string]any{"amount": 100000, "currency": "IQD"})

	e.setResellerPrice(t, agentID, prof, nil, 20000, "IQD")       // plan-wide
	e.setResellerPrice(t, agentID, prof, &subVIP, 15000, "IQD")   // per-subscriber override

	rVIP := e.do(t, "POST", "/api/v1/subscribers/"+subVIP+"/renew", agentTok, map[string]any{})
	if rVIP.status != http.StatusOK {
		t.Fatalf("renew VIP = %d: %s", rVIP.status, rVIP.body)
	}
	if got := e.balanceFor(t, agentID, "IQD"); got != 100000-15000 {
		t.Errorf("balance after VIP renewal = %d, want %d (debited per-subscriber 15000)", got, 100000-15000)
	}

	rOther := e.do(t, "POST", "/api/v1/subscribers/"+subOther+"/renew", agentTok, map[string]any{})
	if rOther.status != http.StatusOK {
		t.Fatalf("renew other = %d: %s", rOther.status, rOther.body)
	}
	if got := e.balanceFor(t, agentID, "IQD"); got != 100000-15000-20000 {
		t.Errorf("balance after both renewals = %d, want %d (other debited plan-wide 20000)", got, 100000-15000-20000)
	}

	// Both subscribers were charged full retail (25000) regardless of either
	// wholesale number.
	var subVIPReceipt, subOtherReceipt int64
	if err := e.db.QueryRow(context.Background(),
		`SELECT amount FROM payments WHERE subscriber_id=$1::uuid ORDER BY at DESC LIMIT 1`, subVIP).Scan(&subVIPReceipt); err != nil {
		t.Fatal(err)
	}
	if err := e.db.QueryRow(context.Background(),
		`SELECT amount FROM payments WHERE subscriber_id=$1::uuid ORDER BY at DESC LIMIT 1`, subOther).Scan(&subOtherReceipt); err != nil {
		t.Fatal(err)
	}
	if subVIPReceipt != 25000 || subOtherReceipt != 25000 {
		t.Errorf("subscriber receipts = (%d, %d), want (25000, 25000) — retail unaffected by wholesale", subVIPReceipt, subOtherReceipt)
	}
}

// TestPriceOverrideIndependentOfWholesale — AC-76a: a subscriber with an
// active price_override belonging to a reseller with a wholesale price
// configured is charged their override exactly (unaffected by the reseller
// relationship), while the reseller's balance still debits the resolved
// wholesale price — the two numbers never conflate.
func TestPriceOverrideIndependentOfWholesale(t *testing.T) {
	e := setup(t)
	prof := e.createProfile(t, 25000, 30) // retail 25000
	sub := e.createSubscriber(t, prof)
	agentID, agentTok := e.seedAgent(t)
	_, _ = e.db.Exec(context.Background(),
		`UPDATE subscribers SET owner_manager_id=$1::uuid, price_override=18000 WHERE id=$2::uuid`, agentID, sub)
	e.do(t, "POST", "/api/v1/managers/"+agentID+"/topup", e.token, map[string]any{"amount": 100000, "currency": "IQD"})
	e.setResellerPrice(t, agentID, prof, nil, 20000, "IQD")

	r := e.do(t, "POST", "/api/v1/subscribers/"+sub+"/renew", agentTok, map[string]any{})
	if r.status != http.StatusOK {
		t.Fatalf("renew = %d: %s", r.status, r.body)
	}

	// Subscriber charged their override (18000), not retail (25000) and not
	// the wholesale price (20000).
	var receiptAmount int64
	if err := e.db.QueryRow(context.Background(),
		`SELECT amount FROM payments WHERE subscriber_id=$1::uuid ORDER BY at DESC LIMIT 1`, sub).Scan(&receiptAmount); err != nil {
		t.Fatal(err)
	}
	if receiptAmount != 18000 {
		t.Errorf("subscriber receipt = %d, want 18000 (their own override)", receiptAmount)
	}
	// Reseller's balance still debited the resolved wholesale price (20000),
	// not the subscriber's override.
	if got := e.balanceFor(t, agentID, "IQD"); got != 100000-20000 {
		t.Errorf("reseller balance = %d, want %d (debited wholesale 20000, unaffected by the subscriber's 18000 override)", got, 100000-20000)
	}
}
