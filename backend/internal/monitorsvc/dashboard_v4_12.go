package monitorsvc

// v4-12 (FR-174) dashboard widgets: clickable KPI tiles (open tickets,
// routers reachable, unpaid invoices), the ops health panel, and the
// recent-activity tables. Same posture as every widget in dashboard_api.go:
// cross-domain reads are raw SQL against tables another package owns, never
// a cross-package Go import (that file's own doc comment states this
// pattern; see revenueToday/nasCards), and every read degrades to a zero/
// empty value on error rather than 500ing the whole dashboard call.

import (
	"context"
	"time"

	"github.com/hikrad/hikrad/internal/auth"
)

// --- KPI tiles ---------------------------------------------------------

// openTicketsCount matches internal/support's own openCount definition
// (status IN ('open','in_progress')) exactly — one definition, not a second
// one that could drift (the same discipline FR-46.1's expiring-subscribers
// query already applies).
func openTicketsCount(ctx context.Context) int64 {
	if pkgDB == nil {
		return 0
	}
	scope := auth.ScopeFilter(ctx)
	q := `SELECT count(*) FROM support_tickets t JOIN subscribers s ON s.id = t.subscriber_id
	       WHERE t.status IN ('open','in_progress')`
	args := []any{}
	if scope != nil {
		q += ` AND s.owner_manager_id = $1::uuid`
		args = append(args, scope.ManagerID)
	}
	var n int64
	if err := pkgDB.QueryRow(ctx, q, args...).Scan(&n); err != nil {
		return 0
	}
	return n
}

type routersReachableTile struct {
	Reachable int64 `json:"reachable"`
	Total     int64 `json:"total"`
}

// routersReachable reuses nasCards (the exact status this module's own
// device grid already computes), not a second reachability query.
func routersReachable(ctx context.Context) routersReachableTile {
	cards := nasCards(ctx)
	out := routersReachableTile{Total: int64(len(cards))}
	for _, c := range cards {
		if c.Status == string(statusUp) {
			out.Reachable++
		}
	}
	return out
}

type unpaidInvoicesTile struct {
	Count          int64 `json:"count"`
	OutstandingIQD int64 `json:"outstanding_iqd"`
}

func unpaidInvoices(ctx context.Context) unpaidInvoicesTile {
	var out unpaidInvoicesTile
	if pkgDB == nil {
		return out
	}
	scope := auth.ScopeFilter(ctx)
	q := `SELECT count(*), COALESCE(sum(i.total - i.amount_paid), 0)
	        FROM invoices i LEFT JOIN subscribers s ON s.id = i.subscriber_id
	       WHERE i.status IN ('unpaid', 'partial', 'due')`
	args := []any{}
	if scope != nil {
		q += ` AND s.owner_manager_id = $1::uuid`
		args = append(args, scope.ManagerID)
	}
	if err := pkgDB.QueryRow(ctx, q, args...).Scan(&out.Count, &out.OutstandingIQD); err != nil {
		return unpaidInvoicesTile{}
	}
	return out
}

// --- Ops health panel ----------------------------------------------------

type opsHealthPanel struct {
	CoAQueueDepth    int64 `json:"coa_queue_depth"`
	CoAFailed24h     int64 `json:"coa_failed_24h"`
	PendingApprovals int64 `json:"pending_approvals"`
	// ThrottledSessions is an approximation, documented as such: open
	// sessions whose most recent limits-worker CoA action (coa_outbox.reason,
	// v4-4's radius/limitsworker.go) was a throttle and hasn't since been
	// superseded by a different reason. It is a health-panel signal, not a
	// billed or displayed-to-subscriber figure (Decision 21 does not apply
	// here — this is an operator ops view, not a subscriber-facing ceiling).
	ThrottledSessions int64 `json:"throttled_sessions"`
	// ActivePlans is a straight count of profiles that exist — profiles carry
	// no enabled/disabled flag in the schema, so "active" here means
	// "offered", not a status filter that does not exist.
	ActivePlans int64 `json:"active_plans"`
	OnlineNow   int64 `json:"online_now"`
	ActiveSubs  int64 `json:"active_subscribers"`
}

func loadOpsHealthPanel(ctx context.Context) opsHealthPanel {
	var p opsHealthPanel
	p.OnlineNow = onlineNow(ctx)
	p.ActiveSubs = subscriberTiles(ctx).Active
	if pkgDB == nil {
		return p
	}
	// Same pending-queue definition FR-152's outbox counters use
	// (coa_outbox_store.go's outboxCounters): a row still awaiting a
	// terminal state.
	_ = pkgDB.QueryRow(ctx,
		`SELECT count(*) FROM coa_outbox WHERE state IN ('pending','sent')`).Scan(&p.CoAQueueDepth)
	_ = pkgDB.QueryRow(ctx,
		`SELECT count(*) FROM coa_outbox WHERE state = 'failed' AND done_at >= $1`,
		time.Now().Add(-24*time.Hour)).Scan(&p.CoAFailed24h)
	_ = pkgDB.QueryRow(ctx,
		`SELECT count(*) FROM approval_requests WHERE state = 'pending'`).Scan(&p.PendingApprovals)
	_ = pkgDB.QueryRow(ctx, `SELECT count(*) FROM profiles`).Scan(&p.ActivePlans)
	_ = pkgDB.QueryRow(ctx, `
		SELECT count(DISTINCT se.id)
		  FROM sessions se
		  JOIN LATERAL (
		      SELECT reason FROM coa_outbox co
		       WHERE co.subscriber_id = se.subscriber_id AND co.acct_session_id = se.acct_session_id
		       ORDER BY co.created_at DESC LIMIT 1
		  ) latest ON true
		 WHERE se.stopped_at IS NULL AND latest.reason = 'limits.throttle'`).Scan(&p.ThrottledSessions)
	return p
}

// --- Recent-activity tables ----------------------------------------------

type recentCoARow struct {
	ID       string    `json:"id"`
	Username string    `json:"username"`
	Action   string    `json:"action"`
	State    string    `json:"state"`
	Reason   string    `json:"reason"`
	At       time.Time `json:"at"`
}

func recentCoAOutbox(ctx context.Context) []recentCoARow {
	out := []recentCoARow{}
	if pkgDB == nil {
		return out
	}
	rows, err := pkgDB.Query(ctx,
		`SELECT id::text, username, action, state, reason, created_at
		   FROM coa_outbox ORDER BY created_at DESC LIMIT 10`)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var r recentCoARow
		if err := rows.Scan(&r.ID, &r.Username, &r.Action, &r.State, &r.Reason, &r.At); err != nil {
			return out
		}
		r.At = r.At.UTC()
		out = append(out, r)
	}
	return out
}

type recentAdminActionRow struct {
	ID       int64     `json:"id"`
	Actor    string    `json:"actor"`
	Action   string    `json:"action"`
	Entity   string    `json:"entity_type"`
	EntityID string    `json:"entity_id"`
	At       time.Time `json:"at"`
}

func recentAdminActions(ctx context.Context) []recentAdminActionRow {
	out := []recentAdminActionRow{}
	if pkgDB == nil {
		return out
	}
	rows, err := pkgDB.Query(ctx, `
		SELECT a.id, COALESCE(m.username, ''), a.action, a.entity_type, a.entity_id, a.at
		  FROM audit_log a LEFT JOIN managers m ON m.id = a.actor_id
		 ORDER BY a.id DESC LIMIT 10`)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var r recentAdminActionRow
		if err := rows.Scan(&r.ID, &r.Actor, &r.Action, &r.Entity, &r.EntityID, &r.At); err != nil {
			return out
		}
		r.At = r.At.UTC()
		out = append(out, r)
	}
	return out
}

type recentSubscriberRow struct {
	ID        string    `json:"id"`
	Username  string    `json:"username"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

func recentSubscribers(ctx context.Context) []recentSubscriberRow {
	out := []recentSubscriberRow{}
	if pkgDB == nil {
		return out
	}
	scope := auth.ScopeFilter(ctx)
	q := `SELECT id::text, username, status, created_at FROM subscribers s`
	args := []any{}
	if scope != nil {
		q += ` WHERE s.owner_manager_id = $1::uuid`
		args = append(args, scope.ManagerID)
	}
	q += ` ORDER BY created_at DESC LIMIT 10`
	rows, err := pkgDB.Query(ctx, q, args...)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var r recentSubscriberRow
		if err := rows.Scan(&r.ID, &r.Username, &r.Status, &r.CreatedAt); err != nil {
			return out
		}
		r.CreatedAt = r.CreatedAt.UTC()
		out = append(out, r)
	}
	return out
}

// recentRouterHealth is the same nasCards() data the existing nas-health
// widget already computes, exposed under this widget's own id for the
// "recent activity" grouping the FR-174 dashboard layout calls for — not a
// second reachability query.
func recentRouterHealth(ctx context.Context) []nasCard {
	cards := nasCards(ctx)
	if cards == nil {
		cards = []nasCard{}
	}
	return cards
}
