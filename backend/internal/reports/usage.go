package reports

// Usage reports (C2, FR-47): top consumers and per-NAS totals, straight
// queries over usage_daily rollups only — never the raw usage_points
// hypertable — so this stays inside the NFR-1 page budget at 5k-subscriber
// scale (C's perf suite verifies against the fixtures this file's tests
// deliver).

import (
	"net/http"
	"strconv"

	"github.com/hikrad/hikrad/internal/auth"
	"github.com/hikrad/hikrad/internal/httpapi"
)

type topConsumerRow struct {
	SubscriberID string `json:"subscriber_id"`
	Username     string `json:"username"`
	DownBytes    int64  `json:"down_bytes"`
	UpBytes      int64  `json:"up_bytes"`
}

type perNASRow struct {
	NASID     string `json:"nas_id"`
	NASName   string `json:"nas_name"`
	DownBytes int64  `json:"down_bytes"`
	UpBytes   int64  `json:"up_bytes"`
}

func (m *Module) usageReportHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	scope := auth.ScopeFilter(ctx)
	view := r.URL.Query().Get("view")
	from, to := parseRange(r)
	limit := 20
	if v, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && v > 0 && v <= 200 {
		limit = v
	}

	switch view {
	case "top_consumers":
		q := `SELECT u.subscriber_id::text, COALESCE(s.username::text,''),
		             sum(u.down_bytes)::bigint, sum(u.up_bytes)::bigint
		        FROM usage_daily u
		        JOIN subscribers s ON s.id = u.subscriber_id
		       WHERE u.day >= $1 AND u.day < $2 AND u.subscriber_id IS NOT NULL`
		args := []any{from, to}
		if scope != nil {
			q += ` AND s.owner_manager_id = $3::uuid`
			args = append(args, scope.ManagerID)
		}
		q += ` GROUP BY u.subscriber_id, s.username ORDER BY sum(u.down_bytes + u.up_bytes) DESC LIMIT $` +
			strconv.Itoa(len(args)+1)
		args = append(args, limit)

		rows, err := m.db.Query(ctx, q, args...)
		if err != nil {
			m.internalError(w, "usage top_consumers", err)
			return
		}
		defer rows.Close()
		out := []topConsumerRow{}
		for rows.Next() {
			var tr topConsumerRow
			if err := rows.Scan(&tr.SubscriberID, &tr.Username, &tr.DownBytes, &tr.UpBytes); err != nil {
				m.internalError(w, "usage top_consumers scan", err)
				return
			}
			out = append(out, tr)
		}
		if err := rows.Err(); err != nil {
			m.internalError(w, "usage top_consumers rows", err)
			return
		}
		if r.URL.Query().Get("format") == "csv" {
			if !requireExport(w, r) {
				return
			}
			recs := make([][]string, len(out))
			for i, tr := range out {
				recs[i] = []string{tr.SubscriberID, tr.Username, itoa64(tr.DownBytes), itoa64(tr.UpBytes)}
			}
			writeCSV(w, "usage_top_consumers.csv", []string{"subscriber_id", "username", "down_bytes", "up_bytes"}, recs)
			return
		}
		httpapi.JSON(w, http.StatusOK, map[string]any{"rows": out})

	case "per_nas":
		// Scoped managers see no per-NAS breakdown (NAS is not subscriber-owned
		// data): restrict the underlying subscriber set the same way, so a
		// scoped caller's per-NAS totals only reflect their own subscribers'
		// usage rather than leaking sitewide NAS traffic.
		q := `SELECT COALESCE(u.nas_id::text,''), COALESCE(n.name,'(unknown)'),
		             sum(u.down_bytes)::bigint, sum(u.up_bytes)::bigint
		        FROM usage_daily u
		        LEFT JOIN nas n ON n.id = u.nas_id`
		args := []any{}
		where := ` WHERE u.day >= $1 AND u.day < $2`
		args = append(args, from, to)
		if scope != nil {
			where += ` AND u.subscriber_id IN (SELECT id FROM subscribers WHERE owner_manager_id = $3::uuid)`
			args = append(args, scope.ManagerID)
		}
		q += where + ` GROUP BY u.nas_id, n.name ORDER BY sum(u.down_bytes + u.up_bytes) DESC`

		rows, err := m.db.Query(ctx, q, args...)
		if err != nil {
			m.internalError(w, "usage per_nas", err)
			return
		}
		defer rows.Close()
		out := []perNASRow{}
		for rows.Next() {
			var pr perNASRow
			if err := rows.Scan(&pr.NASID, &pr.NASName, &pr.DownBytes, &pr.UpBytes); err != nil {
				m.internalError(w, "usage per_nas scan", err)
				return
			}
			out = append(out, pr)
		}
		if err := rows.Err(); err != nil {
			m.internalError(w, "usage per_nas rows", err)
			return
		}
		if r.URL.Query().Get("format") == "csv" {
			if !requireExport(w, r) {
				return
			}
			recs := make([][]string, len(out))
			for i, pr := range out {
				recs[i] = []string{pr.NASID, pr.NASName, itoa64(pr.DownBytes), itoa64(pr.UpBytes)}
			}
			writeCSV(w, "usage_per_nas.csv", []string{"nas_id", "nas_name", "down_bytes", "up_bytes"}, recs)
			return
		}
		httpapi.JSON(w, http.StatusOK, map[string]any{"rows": out})

	default:
		httpapi.Error(w, http.StatusUnprocessableEntity, "validation_failed", "request validation failed",
			httpapi.FieldError{Field: "view", Message: "must be one of: top_consumers per_nas"})
	}
}
