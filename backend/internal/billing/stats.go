package billing

// Subscriber counts for C's dashboard tiles + expiring digests (frozen shape
// GET /internal/stats/subscribers). Unproxied internal route (service-to-service,
// like the RADIUS authorize endpoint) — C reads it to render the subs tile
// (active/expired/expiring-7d) and the expiring-N digest.

import (
	"net/http"
	"strconv"

	"github.com/hikrad/hikrad/internal/httpapi"
)

type subscriberStats struct {
	Active      int `json:"active"`
	Expired     int `json:"expired"`
	Disabled    int `json:"disabled"`
	Expiring7d  int `json:"expiring_7d"`
	ExpiringN   int `json:"expiring_n"`
	ExpiringNDays int `json:"expiring_n_days"`
	Total       int `json:"total"`
}

func (m *Module) subscriberStatsHandler(w http.ResponseWriter, r *http.Request) {
	days := 7
	if v := r.URL.Query().Get("within_days"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 365 {
			days = n
		}
	}
	var s subscriberStats
	s.ExpiringNDays = days
	err := m.db.QueryRow(r.Context(),
		`SELECT
		    count(*) FILTER (WHERE status = 'active')   AS active,
		    count(*) FILTER (WHERE status = 'expired')  AS expired,
		    count(*) FILTER (WHERE status = 'disabled') AS disabled,
		    count(*) FILTER (WHERE status = 'active' AND expires_at IS NOT NULL
		                      AND expires_at > now() AND expires_at <= now() + interval '7 days')  AS exp7,
		    count(*) FILTER (WHERE status = 'active' AND expires_at IS NOT NULL
		                      AND expires_at > now() AND expires_at <= now() + make_interval(days => $1)) AS expn,
		    count(*)                                     AS total
		   FROM subscribers`, days).
		Scan(&s.Active, &s.Expired, &s.Disabled, &s.Expiring7d, &s.ExpiringN, &s.Total)
	if err != nil {
		m.internalError(w, "subscriber stats", err)
		return
	}
	httpapi.JSON(w, http.StatusOK, s)
}
