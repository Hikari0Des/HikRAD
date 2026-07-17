package reports_test

// Gate items for v2 phase 9 (FR-72.3/FR-73.3/FR-75): margin reconciliation,
// per-site overhead isolation, and reseller-scoping never leaking cost data.

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

func (e env) setProfileCost(t *testing.T, profileID string, cost int64, currency string) {
	t.Helper()
	r := e.do(t, "POST", "/api/v1/profiles/"+profileID+"/cost", e.token,
		map[string]any{"cost": cost, "currency": currency})
	if r.status != http.StatusCreated {
		t.Fatalf("set profile cost = %d: %s", r.status, r.body)
	}
}

func (e env) createOverhead(t *testing.T, name string, amount int64, currency string, nasID *string, from, to time.Time) {
	t.Helper()
	body := map[string]any{
		"name": name, "amount": amount, "currency": currency,
		"period_start": from.Format(time.RFC3339), "period_end": to.Format(time.RFC3339),
	}
	if nasID != nil {
		body["nas_id"] = *nasID
	}
	r := e.do(t, "POST", "/api/v1/overheads", e.token, body)
	if r.status != http.StatusCreated {
		t.Fatalf("create overhead = %d: %s", r.status, r.body)
	}
}

// TestMarginReconciliation — AC-72a: sum(margin) = sum(revenue) -
// sum(cost_at_sale) over rows with a known cost; a row with unknown cost
// still counts toward revenue but is excluded from cost/margin.
func TestMarginReconciliation(t *testing.T) {
	e := setup(t)
	from := time.Now().Add(-time.Minute).UTC()

	known := e.createProfile(t, 25000, 30)
	e.setProfileCost(t, known, 10000, "IQD")
	subKnown := e.createSubscriber(t, known)

	unknown := e.createProfile(t, 25000, 30)
	subUnknown := e.createSubscriber(t, unknown)

	if r := e.do(t, "POST", "/api/v1/subscribers/"+subKnown+"/renew", e.token, map[string]any{}); r.status != http.StatusOK {
		t.Fatalf("renew known = %d: %s", r.status, r.body)
	}
	if r := e.do(t, "POST", "/api/v1/subscribers/"+subUnknown+"/renew", e.token, map[string]any{}); r.status != http.StatusOK {
		t.Fatalf("renew unknown = %d: %s", r.status, r.body)
	}

	to := time.Now().Add(time.Minute).UTC()
	r := e.do(t, "GET", "/api/v1/reports/margin?from="+from.Format(time.RFC3339)+"&to="+to.Format(time.RFC3339), e.token, nil)
	if r.status != http.StatusOK {
		t.Fatalf("margin report = %d: %s", r.status, r.body)
	}
	var out struct {
		Rows []struct {
			ProfileID        string `json:"profile_id"`
			Revenue          int64  `json:"revenue"`
			Wholesale        int64  `json:"wholesale"`
			Cost             *int64 `json:"cost"`
			OwnerMargin      *int64 `json:"owner_margin"`
			UnknownCostCount *int   `json:"unknown_cost_count"`
		} `json:"rows"`
	}
	r.into(t, &out)

	var foundKnown, foundUnknown bool
	for _, row := range out.Rows {
		switch row.ProfileID {
		case known:
			foundKnown = true
			if row.Revenue != 25000 {
				t.Errorf("known-cost row revenue = %d, want 25000", row.Revenue)
			}
			if row.Cost == nil || *row.Cost != 10000 {
				t.Errorf("known-cost row cost = %v, want 10000", row.Cost)
			}
			if row.OwnerMargin == nil || *row.OwnerMargin != row.Wholesale-10000 {
				t.Errorf("known-cost row owner_margin = %v, want %d", row.OwnerMargin, row.Wholesale-10000)
			}
			if row.UnknownCostCount == nil || *row.UnknownCostCount != 0 {
				t.Errorf("known-cost row unknown_cost_count = %v, want 0", row.UnknownCostCount)
			}
		case unknown:
			foundUnknown = true
			// Revenue still counts even though cost is unknown (never
			// excluded from revenue, only from margin).
			if row.Revenue != 25000 {
				t.Errorf("unknown-cost row revenue = %d, want 25000 (still counted)", row.Revenue)
			}
			if row.Cost != nil {
				t.Errorf("unknown-cost row cost = %v, want nil (never defaulted to 0)", *row.Cost)
			}
			if row.OwnerMargin != nil {
				t.Errorf("unknown-cost row owner_margin = %v, want nil (cost is unknown)", *row.OwnerMargin)
			}
			if row.UnknownCostCount == nil || *row.UnknownCostCount != 1 {
				t.Errorf("unknown-cost row unknown_cost_count = %v, want 1", row.UnknownCostCount)
			}
		}
	}
	if !foundKnown || !foundUnknown {
		t.Fatalf("expected rows for both profiles, got %+v", out.Rows)
	}
}

// TestSiteMarginNeverBlendsGlobal — AC-73a: a global overhead and a
// per-site overhead in the same period; the per-site net-margin figure nets
// only its own tagged overhead, never a pro-rated share of the global one.
func TestSiteMarginNeverBlendsGlobal(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	from := time.Now().Add(-time.Minute).UTC()

	var nasID string
	if err := e.db.QueryRow(ctx,
		`INSERT INTO nas (name, ip, secret_enc) VALUES ($1, '10.99.0.1'::inet, '\x01'::bytea) RETURNING id::text`,
		uniqName(t)).Scan(&nasID); err != nil {
		t.Fatalf("insert nas: %v", err)
	}

	prof := e.createProfile(t, 25000, 30)
	sub := e.createSubscriber(t, prof)

	// Attribute the subscriber to this NAS via a session (FR-73.4).
	if _, err := e.db.Exec(ctx,
		`INSERT INTO sessions (nas_id, acct_session_id, subscriber_id, username, started_at)
		 VALUES ($1::uuid, $2, $3::uuid, 'x', now())`, nasID, uniqName(t), sub); err != nil {
		t.Fatalf("insert session: %v", err)
	}

	if r := e.do(t, "POST", "/api/v1/subscribers/"+sub+"/renew", e.token, map[string]any{}); r.status != http.StatusOK {
		t.Fatalf("renew = %d: %s", r.status, r.body)
	}

	to := time.Now().Add(time.Minute).UTC()
	e.createOverhead(t, "global uplink", 100000, "IQD", nil, from, to)
	e.createOverhead(t, "site power", 5000, "IQD", &nasID, from, to)

	r := e.do(t, "GET", "/api/v1/reports/margin/sites?from="+from.Format(time.RFC3339)+"&to="+to.Format(time.RFC3339), e.token, nil)
	if r.status != http.StatusOK {
		t.Fatalf("site margin = %d: %s", r.status, r.body)
	}
	var out struct {
		Rows []struct {
			NASID           string `json:"nas_id"`
			Currency        string `json:"currency"`
			Revenue         int64  `json:"revenue"`
			SiteOverheads   int64  `json:"site_overheads"`
			NetMargin       int64  `json:"net_margin"`
			GlobalOverheads int64  `json:"global_overheads"`
		} `json:"rows"`
	}
	r.into(t, &out)

	var found bool
	for _, row := range out.Rows {
		if row.NASID != nasID {
			continue
		}
		found = true
		if row.SiteOverheads != 5000 {
			t.Errorf("site_overheads = %d, want 5000 (only this site's own row)", row.SiteOverheads)
		}
		if row.NetMargin != row.Revenue-5000 {
			t.Errorf("net_margin = %d, want %d (revenue - site's own 5000 only, never the global 100000)", row.NetMargin, row.Revenue-5000)
		}
		if row.GlobalOverheads != 100000 {
			t.Errorf("global_overheads = %d, want 100000 (reported separately)", row.GlobalOverheads)
		}
	}
	if !found {
		t.Fatalf("expected a row for nas %s, got %+v", nasID, out.Rows)
	}
}

func uniqName(t *testing.T) string {
	t.Helper()
	return "sess_" + time.Now().Format("150405.000000000")
}

// TestResellerScopingNeverLeaksCost — AC-75a: a reseller-scoped margin-report
// call never contains cost/owner_margin/unknown_cost_count, verified by raw
// JSON key presence, not just typed-struct nil checks (a struct field could
// be present-but-null; only key absence proves the leak is impossible).
func TestResellerScopingNeverLeaksCost(t *testing.T) {
	e := setup(t)
	from := time.Now().Add(-time.Minute).UTC()

	prof := e.createProfile(t, 25000, 30)
	e.setProfileCost(t, prof, 10000, "IQD")
	agentID, agentTok := e.seedAgent(t)
	sub := e.createSubscriberOwned(t, prof, agentID)
	e.topup(t, agentID, e.token, 25000)

	if r := e.do(t, "POST", "/api/v1/subscribers/"+sub+"/renew", agentTok, map[string]any{}); r.status != http.StatusOK {
		t.Fatalf("renew = %d: %s", r.status, r.body)
	}

	to := time.Now().Add(time.Minute).UTC()
	r := e.do(t, "GET", "/api/v1/reports/margin?from="+from.Format(time.RFC3339)+"&to="+to.Format(time.RFC3339), agentTok, nil)
	if r.status != http.StatusOK {
		t.Fatalf("margin report (reseller) = %d: %s", r.status, r.body)
	}
	var raw map[string]any
	if err := json.Unmarshal(r.body, &raw); err != nil {
		t.Fatal(err)
	}
	rows, _ := raw["rows"].([]any)
	if len(rows) == 0 {
		t.Fatalf("expected at least one row, got %s", r.body)
	}
	for _, rowAny := range rows {
		row, _ := rowAny.(map[string]any)
		for _, forbidden := range []string{"cost", "owner_margin", "unknown_cost_count"} {
			if _, present := row[forbidden]; present {
				t.Errorf("reseller-scoped row contains forbidden key %q: %v", forbidden, row)
			}
		}
		if _, present := row["reseller_margin"]; !present {
			t.Errorf("reseller-scoped row missing reseller_margin (the number they ARE entitled to see): %v", row)
		}
	}
}
