package portalapi

// GET /portal/usage (contract C2 FR-41.3): usage-graph passthrough, scoped to
// the token's own subscriber (IDOR rule — no subscriber_id param exists on
// this route). Reads the same usage_daily rollup C's live/sessions_api.go
// exposes to the panel; this is a read-only cross-domain query, the same
// pattern subscribers/authview.go already uses against ip_pools.

import (
	"net/http"
	"time"

	"github.com/hikrad/hikrad/internal/httpapi"
)

type usagePoint struct {
	T    time.Time `json:"t"`
	Down int64     `json:"down"`
	Up   int64     `json:"up"`
}

func (m *Module) usageHandler(w http.ResponseWriter, r *http.Request) {
	sub, _ := SubscriberFrom(r.Context())
	ctx := r.Context()

	monthly := r.URL.Query().Get("granularity") == "monthly"
	bucket := "1 day"
	defWindow := 30 * 24 * time.Hour
	if monthly {
		bucket = "1 month"
		defWindow = 365 * 24 * time.Hour
	}
	to := parseTimeParam(r.URL.Query().Get("to"), time.Now().UTC())
	from := parseTimeParam(r.URL.Query().Get("from"), to.Add(-defWindow))

	rows, err := m.db.Query(ctx,
		`SELECT time_bucket($1::interval, day) AS b,
		        COALESCE(sum(down_bytes),0), COALESCE(sum(up_bytes),0)
		   FROM usage_daily
		  WHERE subscriber_id = $2::uuid AND day >= $3 AND day < $4
		  GROUP BY b ORDER BY b`,
		bucket, sub.ID, from, to)
	if err != nil {
		m.internalError(w, "usage query", err)
		return
	}
	defer rows.Close()

	items := make([]usagePoint, 0, 64)
	for rows.Next() {
		var p usagePoint
		if err := rows.Scan(&p.T, &p.Down, &p.Up); err != nil {
			m.internalError(w, "usage scan", err)
			return
		}
		p.T = p.T.UTC()
		items = append(items, p)
	}
	if rows.Err() != nil {
		m.internalError(w, "usage rows", rows.Err())
		return
	}
	httpapi.JSON(w, http.StatusOK, map[string]any{"items": items})
}

func parseTimeParam(v string, def time.Time) time.Time {
	if v == "" {
		return def
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02"} {
		if t, err := time.Parse(layout, v); err == nil {
			return t.UTC()
		}
	}
	return def
}
