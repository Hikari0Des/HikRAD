package reports

// FR-46.1/FR-48: the single "expiring in N days" query definition, and the
// daily business-digest composition endpoint C's scheduler consumes instead
// of composing its own summary text (monitorsvc/conditions.go's
// digestSummary currently builds an English sentence inline — this endpoint
// is the FR-48 replacement: numeric fields + a message key/params so
// delivery stays localized, matching every other subscriber-facing message
// in this codebase, per contract C2's "one call, localized-key payload").

import (
	"context"
	"net/http"
	"strconv"

	"github.com/hikrad/hikrad/internal/auth"
	"github.com/hikrad/hikrad/internal/httpapi"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ExpiringSubscribers returns active subscribers whose expiry falls within
// the next `days` days — the exact predicate FR-36's expiring_digest alert
// and this package's `reports/subscribers?view=expiring` both use, so they
// can never report a different row set for the same N and moment (AC-46a).
// scope=nil returns every matching subscriber (C's whole-system digest);
// a non-nil scope restricts to that manager's own subscribers.
func ExpiringSubscribers(ctx context.Context, db *pgxpool.Pool, days int, scope *auth.ManagerScope) ([]subscriberRow, error) {
	q := `SELECT ` + subscriberRowCols + ` FROM subscribers s
	       WHERE s.status = 'active' AND s.expires_at IS NOT NULL
	         AND s.expires_at >= now() AND s.expires_at < now() + make_interval(days => $1)`
	args := []any{days}
	if scope != nil {
		q += ` AND s.owner_manager_id = $2::uuid`
		args = append(args, scope.ManagerID)
	}
	q += ` ORDER BY s.expires_at`
	return querySubscriberRows(ctx, db, q, args)
}

type digestResponse struct {
	MessageKey string         `json:"message_key"`
	Params     map[string]any `json:"params"`
	NewUsers   int            `json:"new_users_today"`
	Renewals   struct {
		Count     int   `json:"count"`
		AmountIQD int64 `json:"amount_iqd"`
	} `json:"renewals_today"`
	RevenueTodayIQD int `json:"revenue_today_iqd"`
	ExpiringSoon    struct {
		Days  int `json:"days"`
		Count int `json:"count"`
	} `json:"expiring_soon"`
	ActiveTotal int `json:"active_total"`
}

// digestHandler serves GET /internal/reports/digest?days=N (unproxied,
// service-to-service — same pattern as billing's /internal/stats/subscribers).
func (m *Module) digestHandler(w http.ResponseWriter, r *http.Request) {
	days := 3
	if v, err := strconv.Atoi(r.URL.Query().Get("days")); err == nil && v > 0 {
		days = v
	}
	ctx := r.Context()
	var resp digestResponse
	resp.ExpiringSoon.Days = days
	resp.MessageKey = "digest.daily_summary"

	todayStart := "(now() AT TIME ZONE 'Asia/Baghdad')::date"
	err := m.db.QueryRow(ctx,
		`SELECT count(*) FROM subscribers WHERE (created_at AT TIME ZONE 'Asia/Baghdad')::date = `+todayStart).
		Scan(&resp.NewUsers)
	if err != nil {
		m.internalError(w, "digest new_users", err)
		return
	}
	err = m.db.QueryRow(ctx,
		`SELECT count(*) FROM ledger_transactions
		  WHERE type IN ('renewal','voucher_redeem') AND (at AT TIME ZONE 'Asia/Baghdad')::date = `+todayStart).
		Scan(&resp.Renewals.Count)
	if err != nil {
		m.internalError(w, "digest renewals count", err)
		return
	}
	// v2 phase 4 (FR-70.2): the daily digest headline stays IQD-scoped —
	// summing across currencies here would produce a meaningless blended
	// number for a plain-text summary message. Non-IQD activity is real and
	// visible in the full revenue/settlement reports, just not this headline.
	err = m.db.QueryRow(ctx,
		`SELECT COALESCE(sum(amount),0)::bigint FROM payments
		  WHERE method IN ('renewal','cash','voucher') AND currency = 'IQD' AND (at AT TIME ZONE 'Asia/Baghdad')::date = `+todayStart).
		Scan(&resp.Renewals.AmountIQD)
	if err != nil {
		m.internalError(w, "digest renewals amount", err)
		return
	}
	var revenue int64
	err = m.db.QueryRow(ctx,
		`SELECT COALESCE(sum(amount),0)::bigint FROM payments
		  WHERE currency = 'IQD' AND (at AT TIME ZONE 'Asia/Baghdad')::date = `+todayStart).
		Scan(&revenue)
	if err != nil {
		m.internalError(w, "digest revenue", err)
		return
	}
	resp.RevenueTodayIQD = int(revenue)
	err = m.db.QueryRow(ctx, `SELECT count(*) FROM subscribers WHERE status = 'active'`).Scan(&resp.ActiveTotal)
	if err != nil {
		m.internalError(w, "digest active total", err)
		return
	}
	expiring, err := ExpiringSubscribers(ctx, m.db, days, nil)
	if err != nil {
		m.internalError(w, "digest expiring", err)
		return
	}
	resp.ExpiringSoon.Count = len(expiring)
	resp.Params = map[string]any{
		"new_users": resp.NewUsers, "renewals": resp.Renewals.Count, "revenue_iqd": resp.RevenueTodayIQD,
		"expiring_days": days, "expiring_count": resp.ExpiringSoon.Count, "active_total": resp.ActiveTotal,
	}
	httpapi.JSON(w, http.StatusOK, resp)
}
