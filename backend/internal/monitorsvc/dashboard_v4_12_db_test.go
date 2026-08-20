package monitorsvc_test

// v4-12 (FR-174) DB-backed suite: the new KPI tiles, ops health panel, and
// recent-activity tables, plus their permission gating — same env/setup
// harness db_test.go already established for this package.

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"testing"
)

func uniqIP(t *testing.T) string {
	t.Helper()
	a, _ := rand.Int(rand.Reader, big.NewInt(254))
	b, _ := rand.Int(rand.Reader, big.NewInt(254))
	return fmt.Sprintf("10.98.%d.%d", a.Int64()+1, b.Int64()+1)
}

func TestDashboardV4_12_KPITilesAndOpsHealth(t *testing.T) {
	e := setup(t)
	ctx := context.Background()

	// A NAS with no reachability probes -> counted as not "up" by
	// routersReachable (nasCards defaults an unprobed NAS's status away from
	// "up"), so total=1 is enough to prove the tile counts something real
	// without needing a live ICMP probe in this sandbox.
	var nasID string
	if err := e.db.QueryRow(ctx,
		`INSERT INTO nas (name, ip, secret_enc, enabled) VALUES ($1, $2::inet, '\x01'::bytea, true) RETURNING id::text`,
		uniq("nas_"), uniqIP(t)).Scan(&nasID); err != nil {
		t.Fatalf("seed nas: %v", err)
	}

	// A subscriber with an open support ticket and an unpaid invoice.
	var profID, subID string
	if err := e.db.QueryRow(ctx,
		`INSERT INTO profiles (name, currency, price, duration_days, rate_down_kbps, rate_up_kbps)
		 VALUES ($1,'IQD',10000,30,10240,2048) RETURNING id::text`, uniq("plan_")).Scan(&profID); err != nil {
		t.Fatalf("seed profile: %v", err)
	}
	if err := e.db.QueryRow(ctx,
		`INSERT INTO subscribers (username, profile_id, status) VALUES ($1,$2::uuid,'active') RETURNING id::text`,
		uniq("u_"), profID).Scan(&subID); err != nil {
		t.Fatalf("seed subscriber: %v", err)
	}
	if _, err := e.db.Exec(ctx,
		`INSERT INTO support_tickets (number, subscriber_id, subject, priority, status, created_by_type)
		 VALUES ($1, $2::uuid, 'test ticket', 'normal', 'open', 'subscriber')`, uniq("TCK-"), subID); err != nil {
		t.Fatalf("seed ticket: %v", err)
	}
	if _, err := e.db.Exec(ctx,
		`INSERT INTO invoices (invoice_no, subscriber_id, mode, due_date, total, amount_paid, status)
		 VALUES ($1, $2::uuid, 'full', CURRENT_DATE, 15000, 0, 'unpaid')`,
		uniq("inv_"), subID); err != nil {
		t.Fatalf("seed invoice: %v", err)
	}

	// A pending approval request.
	if _, err := e.db.Exec(ctx,
		`INSERT INTO approval_requests (kind, payload, requested_by, description, state)
		 VALUES ('undo', '{}'::jsonb, (SELECT id FROM managers LIMIT 1), 'test', 'pending')`); err != nil {
		t.Fatalf("seed approval: %v", err)
	}

	// An open session throttled by the limits worker (coa_outbox.reason =
	// 'limits.throttle'), matching loadOpsHealthPanel's exact query.
	sessAcctID := uniq("sess_")
	if _, err := e.db.Exec(ctx,
		`INSERT INTO sessions (nas_id, acct_session_id, subscriber_id, username, started_at)
		 VALUES ($1::uuid, $2, $3::uuid, 'x', now())`, nasID, sessAcctID, subID); err != nil {
		t.Fatalf("seed session: %v", err)
	}
	if _, err := e.db.Exec(ctx,
		`INSERT INTO coa_outbox (subscriber_id, nas_id, acct_session_id, username, action, current_op, reason, state)
		 VALUES ($1::uuid, $2::uuid, $3, 'x', 'update', 'update', 'limits.throttle', 'done')`,
		subID, nasID, sessAcctID); err != nil {
		t.Fatalf("seed coa_outbox: %v", err)
	}

	q := "/api/v1/dashboard?widgets=" + widgetsCSV([]string{
		"open-tickets", "routers-reachable", "unpaid-invoices", "ops-health-panel",
		"recent-coa-outbox", "recent-admin-actions", "recent-subscribers", "recent-router-health",
	})
	r := e.do(t, "GET", q, e.adminToken, nil)
	if r.status != http.StatusOK {
		t.Fatalf("dashboard v4-12 widgets = %d: %s", r.status, r.body)
	}

	var out struct {
		OpenTickets      int64 `json:"open_tickets"`
		RoutersReachable struct {
			Reachable int64 `json:"reachable"`
			Total     int64 `json:"total"`
		} `json:"routers_reachable"`
		UnpaidInvoices struct {
			Count          int64 `json:"count"`
			OutstandingIQD int64 `json:"outstanding_iqd"`
		} `json:"unpaid_invoices"`
		OpsHealth struct {
			CoAQueueDepth     int64 `json:"coa_queue_depth"`
			CoAFailed24h      int64 `json:"coa_failed_24h"`
			PendingApprovals  int64 `json:"pending_approvals"`
			ThrottledSessions int64 `json:"throttled_sessions"`
			ActivePlans       int64 `json:"active_plans"`
		} `json:"ops_health"`
		RecentCoAOutbox []struct {
			Username string `json:"username"`
			Reason   string `json:"reason"`
		} `json:"recent_coa_outbox"`
		RecentSubscribers []struct {
			ID string `json:"id"`
		} `json:"recent_subscribers"`
		RecentRouterHealth []struct {
			ID string `json:"id"`
		} `json:"recent_router_health"`
	}
	r.into(t, &out)

	if out.OpenTickets < 1 {
		t.Errorf("open_tickets = %d, want >= 1 (seeded one)", out.OpenTickets)
	}
	if out.RoutersReachable.Total < 1 {
		t.Errorf("routers_reachable.total = %d, want >= 1", out.RoutersReachable.Total)
	}
	if out.UnpaidInvoices.Count < 1 || out.UnpaidInvoices.OutstandingIQD < 15000 {
		t.Errorf("unpaid_invoices = %+v, want count>=1 and outstanding>=15000", out.UnpaidInvoices)
	}
	if out.OpsHealth.PendingApprovals < 1 {
		t.Errorf("ops_health.pending_approvals = %d, want >= 1", out.OpsHealth.PendingApprovals)
	}
	if out.OpsHealth.ThrottledSessions < 1 {
		t.Errorf("ops_health.throttled_sessions = %d, want >= 1 (seeded one open session with the most-recent CoA reason 'limits.throttle')", out.OpsHealth.ThrottledSessions)
	}
	if out.OpsHealth.ActivePlans < 1 {
		t.Errorf("ops_health.active_plans = %d, want >= 1", out.OpsHealth.ActivePlans)
	}
	foundCoA := false
	for _, row := range out.RecentCoAOutbox {
		if row.Reason == "limits.throttle" {
			foundCoA = true
		}
	}
	if !foundCoA {
		t.Errorf("recent_coa_outbox missing the seeded limits.throttle row: %+v", out.RecentCoAOutbox)
	}
	foundSub := false
	for _, row := range out.RecentSubscribers {
		if row.ID == subID {
			foundSub = true
		}
	}
	if !foundSub {
		t.Errorf("recent_subscribers missing the seeded subscriber: %+v", out.RecentSubscribers)
	}
	foundNAS := false
	for _, row := range out.RecentRouterHealth {
		if row.ID == nasID {
			foundNAS = true
		}
	}
	if !foundNAS {
		t.Errorf("recent_router_health missing the seeded nas: %+v", out.RecentRouterHealth)
	}
}

// TestDashboardV4_12_WidgetsPermissionGating: an agent (subscribers.view,
// reports.view, renew — same builtin role dashboard_gate_test.go's own
// TestDashboardWidgetsPermissionGating documents) sees recent-subscribers
// (has subscribers.view) but none of the other five new widgets, which
// require permissions the agent role lacks. Absent, not empty/null (FR-89.3,
// unchanged by this phase).
func TestDashboardV4_12_WidgetsPermissionGating(t *testing.T) {
	e := setup(t)
	_, agentToken := e.createManager(t, "agent", true)

	q := "/api/v1/dashboard?widgets=" + widgetsCSV([]string{
		"open-tickets", "routers-reachable", "unpaid-invoices", "ops-health-panel",
		"recent-coa-outbox", "recent-admin-actions", "recent-subscribers", "recent-router-health",
	})
	r := e.do(t, "GET", q, agentToken, nil)
	if r.status != http.StatusOK {
		t.Fatalf("dashboard (agent) = %d: %s", r.status, r.body)
	}
	var body map[string]json.RawMessage
	r.into(t, &body)

	if _, ok := body["recent_subscribers"]; !ok {
		t.Errorf("agent should see recent_subscribers (holds subscribers.view): %s", r.body)
	}
	forbidden := []string{
		"open_tickets", "routers_reachable", "unpaid_invoices", "ops_health",
		"recent_coa_outbox", "recent_admin_actions", "recent_router_health",
	}
	for _, k := range forbidden {
		if _, ok := body[k]; ok {
			t.Errorf("agent response leaked forbidden key %q: %s", k, r.body)
		}
	}
}

func widgetsCSV(ids []string) string {
	out := ""
	for i, id := range ids {
		if i > 0 {
			out += ","
		}
		out += id
	}
	return out
}
