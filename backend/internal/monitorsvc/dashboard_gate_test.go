package monitorsvc_test

// v2-10 gate tests (FR-89/90, contract C1/C3). Uses the db_test.go harness.

import (
	"encoding/json"
	"net/http"
	"testing"
)

var allCatalogIDs = []string{
	"online-now", "revenue-today", "radius-rps",
	"subs-active", "subs-expired", "subs-expiring",
	"pipeline-health", "nas-health",
	"my-balance", "pending-payment-tickets", "alerts-feed",
}

func widgetsQuery(ids []string) string {
	q := ""
	for i, id := range ids {
		if i > 0 {
			q += ","
		}
		q += id
	}
	return "/api/v1/dashboard?widgets=" + q
}

// TestDashboardWidgetsPermissionGating — gate item 2: the builtin agent
// role's actual permission set (subscribers.view, reports.view, renew — NOT
// monitoring.view/live.view/nas.view/payment_tickets.verify, per
// internal/auth/permissions.go's rolePermissions[RoleAgent]) sees only
// subs/revenue/my-balance, never online-now/pipeline/nas-health/alerts-feed/
// pending-payment-tickets.
func TestDashboardWidgetsPermissionGating(t *testing.T) {
	e := setup(t)
	_, agentToken := e.createManager(t, "agent", true)

	r := e.do(t, "GET", widgetsQuery(allCatalogIDs), agentToken, nil)
	if r.status != http.StatusOK {
		t.Fatalf("dashboard (agent) = %d: %s", r.status, r.body)
	}
	var body map[string]json.RawMessage
	r.into(t, &body)

	mustHave := []string{"subs", "revenue_today_iqd", "my_balance"}
	for _, k := range mustHave {
		if _, ok := body[k]; !ok {
			t.Errorf("agent response missing permitted key %q: %s", k, r.body)
		}
	}
	mustNotHave := []string{
		"online_now", "online_24h_sparkline", "radius_rps",
		"pipeline", "nas_cards", "alerts_feed", "pending_payment_tickets",
	}
	for _, k := range mustNotHave {
		if _, ok := body[k]; ok {
			t.Errorf("agent response leaked forbidden key %q (agent lacks the permission): %s", k, r.body)
		}
	}
}

// TestDashboardForbiddenWidgetAbsent — gate item 3: a forbidden or unknown
// widget id in ?widgets= never 400s/403s the call; the key is simply missing
// from a 200.
func TestDashboardForbiddenWidgetAbsent(t *testing.T) {
	e := setup(t)
	_, agentToken := e.createManager(t, "agent", true)

	r := e.do(t, "GET", widgetsQuery([]string{"nas-health", "subs-active", "not-a-real-widget"}), agentToken, nil)
	if r.status != http.StatusOK {
		t.Fatalf("status = %d, want 200 even with a forbidden+unknown id: %s", r.status, r.body)
	}
	var body map[string]json.RawMessage
	r.into(t, &body)
	if _, ok := body["nas_cards"]; ok {
		t.Errorf("forbidden widget (agent lacks nas.view) was not dropped: %s", r.body)
	}
	if _, ok := body["subs"]; !ok {
		t.Errorf("permitted widget missing from an otherwise-mixed request: %s", r.body)
	}
	// The unknown id produced no key and, more importantly, did not error the
	// whole call — status 200 above already proves it, but assert no stray
	// key resembling it slipped through either.
	if _, ok := body["not-a-real-widget"]; ok {
		t.Errorf("unknown widget id should never appear as a key: %s", r.body)
	}
}

// TestDashboardDefaultEqualsToday — gate item 5: requesting every catalog id
// (what "no stored layout" resolves to, FR-90.1) returns, for the fields the
// legacy full aggregate already has, exactly the same values as the legacy
// endpoint — proving "unset behaves like v1 today" is true, not just claimed.
func TestDashboardDefaultEqualsToday(t *testing.T) {
	e := setup(t)

	legacy := e.do(t, "GET", "/api/v1/dashboard", e.adminToken, nil)
	if legacy.status != http.StatusOK {
		t.Fatalf("legacy dashboard = %d: %s", legacy.status, legacy.body)
	}
	var legacyBody map[string]json.RawMessage
	legacy.into(t, &legacyBody)

	filtered := e.do(t, "GET", widgetsQuery(allCatalogIDs), e.adminToken, nil)
	if filtered.status != http.StatusOK {
		t.Fatalf("filtered dashboard = %d: %s", filtered.status, filtered.body)
	}
	var filteredBody map[string]json.RawMessage
	filtered.into(t, &filteredBody)

	for _, k := range []string{"subs", "revenue_today_iqd", "nas_cards", "radius_rps", "pipeline"} {
		lv, ok := legacyBody[k]
		if !ok {
			t.Fatalf("legacy response missing expected key %q", k)
		}
		fv, ok := filteredBody[k]
		if !ok {
			t.Fatalf("default-layout (all-catalog) response missing key %q an admin should see", k)
		}
		if string(lv) != string(fv) {
			t.Errorf("key %q differs between legacy and default-layout responses: legacy=%s filtered=%s", k, lv, fv)
		}
	}
	// The three new widgets exist only in the filtered path — proving the
	// default layout is a strict superset, not a divergent shape.
	for _, k := range []string{"my_balance", "pending_payment_tickets", "alerts_feed"} {
		if _, ok := filteredBody[k]; !ok {
			t.Errorf("default-layout response missing new widget key %q", k)
		}
	}
}

// TestDashboardBackwardCompatibleNoWidgetsParam — gate item 8: GET
// /api/v1/dashboard with no ?widgets= is byte-for-byte unchanged in SHAPE
// from the pre-v2-10 response — exactly the original 7 keys, never the three
// new ones, and still gated on monitoring.view (403 for a manager without it,
// matching the old route-level auth.Require(PermView) exactly).
func TestDashboardBackwardCompatibleNoWidgetsParam(t *testing.T) {
	e := setup(t)

	admin := e.do(t, "GET", "/api/v1/dashboard", e.adminToken, nil)
	if admin.status != http.StatusOK {
		t.Fatalf("admin legacy dashboard = %d: %s", admin.status, admin.body)
	}
	var body map[string]json.RawMessage
	admin.into(t, &body)

	wantKeys := map[string]bool{
		"online_now": true, "online_24h_sparkline": true, "subs": true,
		"revenue_today_iqd": true, "nas_cards": true, "radius_rps": true, "pipeline": true,
	}
	for k := range wantKeys {
		if _, ok := body[k]; !ok {
			t.Errorf("legacy response missing original key %q: %s", k, admin.body)
		}
	}
	for k := range body {
		if !wantKeys[k] {
			t.Errorf("legacy response gained an unexpected key %q — not byte-for-byte compatible: %s", k, admin.body)
		}
	}

	// An agent (no monitoring.view) hitting the legacy path is refused
	// exactly like before this phase — the permission gate did not loosen.
	_, agentToken := e.createManager(t, "agent", true)
	forbidden := e.do(t, "GET", "/api/v1/dashboard", agentToken, nil)
	if forbidden.status != http.StatusForbidden {
		t.Fatalf("legacy dashboard for a manager without monitoring.view = %d, want 403: %s", forbidden.status, forbidden.body)
	}
}
