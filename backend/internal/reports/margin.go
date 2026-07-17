package reports

// Margin report (v2 phase 9, FR-72.3/FR-73/FR-75, contract C9). Per-plan and
// per-period revenue/cost/margin, reconciling to the ledger exactly as FR-40
// already requires of every other reported figure: sum(margin) =
// sum(revenue) - sum(cost_at_sale) over rows with a known cost; rows with an
// unknown cost still count toward revenue, never toward margin.
//
// FR-75's reseller scoping is enforced here, not just in the panel: a scoped
// (reseller) caller's response omits Cost/UnknownCostCount/OwnerMargin
// entirely (Go's `omitempty` on nil pointers means the JSON key is genuinely
// ABSENT, not null) — those fields are simply never populated for a scoped
// caller, so there is no risk of a serialization bug leaking them. This is
// the field-level cut C8 requires on top of ScopeFilter's existing row-level
// cut (a reseller's own renewals only).

import (
	"net/http"

	"github.com/hikrad/hikrad/internal/auth"
	"github.com/hikrad/hikrad/internal/httpapi"
)

type marginRow struct {
	ProfileID      string `json:"profile_id"`
	ProfileName    string `json:"profile_name"`
	Currency       string `json:"currency"`
	Revenue        int64  `json:"revenue"`      // retail billed (payments.amount)
	Wholesale      int64  `json:"wholesale"`     // what actually moved the actor's balance
	Count          int    `json:"count"`
	ResellerMargin int64  `json:"reseller_margin"` // revenue - wholesale; always present

	// Owner-only (FR-75.2/FR-75.3) — left nil (and therefore ABSENT from the
	// JSON, via omitempty) for every scoped/reseller caller.
	Cost             *int64 `json:"cost,omitempty"`
	UnknownCostCount *int   `json:"unknown_cost_count,omitempty"`
	OwnerMargin      *int64 `json:"owner_margin,omitempty"` // wholesale - cost; nil when cost is wholly unknown
}

// marginHandler serves GET /reports/margin?from=&to= (permission
// reports.view; scoped per auth.ScopeFilter — FR-75).
func (m *Module) marginHandler(w http.ResponseWriter, r *http.Request) {
	from, to := parseRange(r)
	scope := auth.ScopeFilter(r.Context())
	scoped := scope != nil

	q := `SELECT p.id::text, p.name, l.currency,
	             COALESCE(sum(pay.amount),0)::bigint AS revenue,
	             COALESCE(sum(-l.amount),0)::bigint AS wholesale,
	             count(*)::int AS cnt,
	             sum(l.cost_at_sale) FILTER (WHERE l.cost_at_sale IS NOT NULL)::bigint AS cost,
	             count(*) FILTER (WHERE l.cost_at_sale IS NULL)::int AS unknown_cost
	        FROM ledger_transactions l
	        JOIN payments pay ON pay.ledger_tx_id = l.id
	        JOIN subscribers s ON s.id = l.subscriber_id
	        JOIN profiles p ON p.id = s.profile_id
	       WHERE l.type = 'renewal' AND l.at >= $1 AND l.at < $2`
	args := []any{from, to}
	if scoped {
		q += ` AND l.actor_manager_id = $3::uuid`
		args = append(args, scope.ManagerID)
	}
	q += ` GROUP BY p.id, p.name, l.currency ORDER BY p.name, l.currency`

	rows, err := m.db.Query(r.Context(), q, args...)
	if err != nil {
		m.internalError(w, "margin", err)
		return
	}
	defer rows.Close()

	out := []marginRow{}
	for rows.Next() {
		var row marginRow
		var cost *int64
		var unknownCost int
		if err := rows.Scan(&row.ProfileID, &row.ProfileName, &row.Currency,
			&row.Revenue, &row.Wholesale, &row.Count, &cost, &unknownCost); err != nil {
			m.internalError(w, "margin scan", err)
			return
		}
		row.ResellerMargin = row.Revenue - row.Wholesale
		if !scoped {
			row.UnknownCostCount = &unknownCost
			row.Cost = cost
			if cost != nil {
				owner := row.Wholesale - *cost
				row.OwnerMargin = &owner
			}
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		m.internalError(w, "margin rows", err)
		return
	}
	httpapi.JSON(w, http.StatusOK, map[string]any{"rows": out})
}

type siteMarginRow struct {
	NASID           string `json:"nas_id"`
	NASName         string `json:"nas_name"`
	Currency        string `json:"currency"`
	Revenue         int64  `json:"revenue"`
	SiteOverheads   int64  `json:"site_overheads"`
	NetMargin       int64  `json:"net_margin"` // revenue - site_overheads ONLY (never global's share)
	GlobalOverheads int64  `json:"global_overheads"`
}

// siteMarginHandler serves GET /reports/margin/sites?from=&to= (FR-73.3): a
// per-NAS net-margin figure that nets ONLY that site's own tagged overheads
// against that site's own attributed revenue — global (nas_id IS NULL)
// overheads are reported alongside as one whole-business figure, never
// pro-rated into any site's number.
func (m *Module) siteMarginHandler(w http.ResponseWriter, r *http.Request) {
	from, to := parseRange(r)
	scope := auth.ScopeFilter(r.Context())

	// FR-73.4: a subscriber is attributed to their most recent NAS as of the
	// period end — one with no session history in the period is excluded
	// from every site's revenue, never guessed into one.
	q := `WITH subscriber_nas AS (
	          SELECT DISTINCT ON (subscriber_id) subscriber_id, nas_id
	            FROM sessions
	           WHERE subscriber_id IS NOT NULL AND nas_id IS NOT NULL AND started_at <= $2
	           ORDER BY subscriber_id, started_at DESC
	      ),
	      revenue AS (
	          SELECT sn.nas_id, l.currency, COALESCE(sum(pay.amount),0)::bigint AS revenue
	            FROM ledger_transactions l
	            JOIN payments pay ON pay.ledger_tx_id = l.id
	            JOIN subscriber_nas sn ON sn.subscriber_id = l.subscriber_id
	           WHERE l.type = 'renewal' AND l.at >= $1 AND l.at < $2`
	args := []any{from, to}
	if scope != nil {
		q += ` AND l.actor_manager_id = $3::uuid`
		args = append(args, scope.ManagerID)
	}
	q += `      GROUP BY sn.nas_id, l.currency
	      )
	      SELECT n.id::text, n.name, rv.currency, rv.revenue,
	             COALESCE((SELECT sum(o.amount) FROM overheads o
	                        WHERE o.nas_id = n.id AND o.currency = rv.currency
	                          AND o.period_start <= $2 AND (o.period_end IS NULL OR o.period_end >= $1)), 0)::bigint AS site_overheads
	        FROM revenue rv JOIN nas n ON n.id = rv.nas_id
	       ORDER BY n.name, rv.currency`

	rows, err := m.db.Query(r.Context(), q, args...)
	if err != nil {
		m.internalError(w, "site margin", err)
		return
	}
	out := []siteMarginRow{}
	for rows.Next() {
		var row siteMarginRow
		if err := rows.Scan(&row.NASID, &row.NASName, &row.Currency, &row.Revenue, &row.SiteOverheads); err != nil {
			rows.Close()
			m.internalError(w, "site margin scan", err)
			return
		}
		row.NetMargin = row.Revenue - row.SiteOverheads
		out = append(out, row)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		m.internalError(w, "site margin rows", err)
		return
	}

	// Global overheads: reported separately, per currency, never merged into
	// any site's net_margin above (FR-73.3).
	globalArgs := []any{from, to}
	globalQ := `SELECT currency, COALESCE(sum(amount),0)::bigint FROM overheads
	             WHERE nas_id IS NULL AND period_start <= $2 AND (period_end IS NULL OR period_end >= $1)
	             GROUP BY currency`
	grows, err := m.db.Query(r.Context(), globalQ, globalArgs...)
	if err != nil {
		m.internalError(w, "global overheads", err)
		return
	}
	globalOverheads := map[string]int64{}
	for grows.Next() {
		var cur string
		var amt int64
		if err := grows.Scan(&cur, &amt); err != nil {
			grows.Close()
			m.internalError(w, "global overheads scan", err)
			return
		}
		globalOverheads[cur] = amt
	}
	grows.Close()
	for i := range out {
		out[i].GlobalOverheads = globalOverheads[out[i].Currency]
	}

	httpapi.JSON(w, http.StatusOK, map[string]any{"rows": out, "global_overheads": globalOverheadsList(globalOverheads)})
}

func globalOverheadsList(m map[string]int64) []map[string]any {
	out := make([]map[string]any, 0, len(m))
	for cur, amt := range m {
		out = append(out, map[string]any{"currency": cur, "amount": amt})
	}
	return out
}
