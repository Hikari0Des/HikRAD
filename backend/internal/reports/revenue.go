package reports

// Revenue report (C2, FR-45.1). Base = payments (the gross customer-billed
// record, refunds already negative — see billing/renew.go and refund.go),
// joined to ledger_transactions for the acting manager and subscribers for
// the current profile. This is deliberately the same table revenue_daily
// (migration 0201, "frozen read-only revenue view ... for reports FR-45")
// was built for — the property test in db_test.go proves this report's
// total reconciles exactly with a direct sum over payments for the same
// range, which is the concrete, testable form of "reports never compute
// money independently" for this endpoint.

import (
	"net/http"

	"github.com/hikrad/hikrad/internal/auth"
	"github.com/hikrad/hikrad/internal/httpapi"
)

var revenueGroupExpr = map[string]string{
	"day":     `to_char((p.at AT TIME ZONE 'Asia/Baghdad')::date, 'YYYY-MM-DD')`,
	"month":   `to_char(date_trunc('month', p.at AT TIME ZONE 'Asia/Baghdad'), 'YYYY-MM')`,
	"manager": `COALESCE(l.actor_manager_id::text, '')`,
	"profile": `COALESCE(s.profile_id::text, '')`,
	"method":  `p.method`,
}

type revenueRow struct {
	Key       string `json:"key"`
	AmountIQD int64  `json:"amount_iqd"`
	Count     int    `json:"count"`
}

// revenueQuery builds the shared SELECT for both the JSON and CSV paths.
func revenueQuery(groupBy string, scope *auth.ManagerScope) (string, string) {
	keyExpr, ok := revenueGroupExpr[groupBy]
	if !ok {
		keyExpr = revenueGroupExpr["day"]
		groupBy = "day"
	}
	where := " WHERE p.at >= $1 AND p.at < $2"
	if scope != nil {
		where += " AND l.actor_manager_id = $3::uuid"
	}
	q := `SELECT ` + keyExpr + ` AS key, sum(p.amount_iqd)::bigint AS amount, count(*)::int AS cnt
	        FROM payments p
	        JOIN ledger_transactions l ON l.id = p.ledger_tx_id
	        LEFT JOIN subscribers s ON s.id = p.subscriber_id` +
		where + ` GROUP BY key ORDER BY key`
	return q, groupBy
}

func (m *Module) revenueHandler(w http.ResponseWriter, r *http.Request) {
	from, to := parseRange(r)
	groupBy := r.URL.Query().Get("group_by")
	scope := auth.ScopeFilter(r.Context())

	q, _ := revenueQuery(groupBy, scope)
	args := []any{from, to}
	if scope != nil {
		args = append(args, scope.ManagerID)
	}
	rows, err := m.db.Query(r.Context(), q, args...)
	if err != nil {
		m.internalError(w, "revenue", err)
		return
	}
	defer rows.Close()

	out := []revenueRow{}
	var total int64
	for rows.Next() {
		var rr revenueRow
		if err := rows.Scan(&rr.Key, &rr.AmountIQD, &rr.Count); err != nil {
			m.internalError(w, "revenue scan", err)
			return
		}
		total += rr.AmountIQD
		out = append(out, rr)
	}
	if err := rows.Err(); err != nil {
		m.internalError(w, "revenue rows", err)
		return
	}

	if r.URL.Query().Get("format") == "csv" {
		if !requireExport(w, r) {
			return
		}
		recs := make([][]string, len(out))
		for i, rr := range out {
			recs[i] = []string{rr.Key, itoa64(rr.AmountIQD), itoa(rr.Count)}
		}
		writeCSV(w, "revenue.csv", []string{"key", "amount_iqd", "count"}, recs)
		return
	}
	httpapi.JSON(w, http.StatusOK, map[string]any{"total_iqd": total, "rows": out})
}
