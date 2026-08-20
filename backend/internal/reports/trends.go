package reports

// Trend lines, daily traffic, and top-10 lists (FR-173). Traffic figures
// (daily traffic, top-10) read usage_daily only — the same rollup usage.go's
// top_consumers already reads, never the raw usage_points hypertable, per
// FR-47's panel-load budget rule. Trend lines need no new storage: "active"/
// "expired" per historical day are derived from subscribers.created_at/
// expires_at (a set-based FILTER count, not a snapshot table — see the
// function doc below for why), and "online sessions" per day is a daily
// bucket of the same online_samples table dashboard_api.go's 24h sparkline
// already reads (monitorsvc/selfcheck.go writes one row/minute).

import (
	"net/http"
	"time"

	"github.com/hikrad/hikrad/internal/auth"
	"github.com/hikrad/hikrad/internal/httpapi"
)

type trendRow struct {
	Date    string `json:"date"`
	Active  int64  `json:"active_subscribers"`
	Online  int64  `json:"online_sessions"`
	Expired int64  `json:"expired_subscribers"`
}

// trendsHandler answers GET /api/v1/reports/trends?from=&to=. "Active"/
// "expired" are derived retroactively from subscribers.created_at/expires_at
// rather than a daily snapshot table — a snapshot would only have history
// from whichever day it started being written, while this derivation has
// real history back to whenever the data starts. It necessarily ignores
// manual status transitions (suspend/re-enable) that leave no historical
// trace anywhere in the schema; that is a real, documented limitation, not
// an oversight (see the gate result). Scoped managers see only their own
// subscribers, same ScopeFilter posture as every other report here.
func (m *Module) trendsHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	scope := auth.ScopeFilter(ctx)
	from, to := parseRange(r)
	if from.Equal(epoch) {
		from = to.AddDate(0, 0, -30)
	}

	// SELECT d.day::date (not bare d.day): generate_series($1::date,
	// $2::date, interval) resolves to timestamptz, not the plain date its
	// arguments suggest — Postgres has no generate_series(date,date,
	// interval) overload, so it implicit-casts date->timestamptz using the
	// SESSION timezone (Asia/Baghdad) to build each bucket, then pgx decodes
	// that timestamptz back into a Go time.Time in UTC. Formatting that
	// UTC-located value with "2006-01-02" silently shifts the calendar date
	// backward by Baghdad's UTC offset (found while building this suite: a
	// day bucket meant to be "Aug 18" rendered as "Aug 17" once round-tripped
	// through Go). Casting back to ::date here re-enters plain-date space
	// (no timezone attached), which pgx decodes without any instant
	// translation — the same reason s.created_at::date/s.expires_at::date
	// below are safe.
	subQ := `SELECT d.day::date,
	    count(*) FILTER (WHERE s.created_at::date <= d.day AND (s.expires_at IS NULL OR s.expires_at::date >= d.day)) AS active,
	    count(*) FILTER (WHERE s.created_at::date <= d.day AND s.expires_at IS NOT NULL AND s.expires_at::date < d.day) AS expired
	  FROM generate_series($1::date, $2::date, interval '1 day') AS d(day)
	  LEFT JOIN subscribers s ON s.created_at::date <= $2::date`
	args := []any{from, to}
	if scope != nil {
		subQ += ` AND s.owner_manager_id = $3::uuid`
		args = append(args, scope.ManagerID)
	}
	subQ += ` GROUP BY d.day ORDER BY d.day`

	rows, err := m.db.Query(ctx, subQ, args...)
	if err != nil {
		m.internalError(w, "trends subscribers", err)
		return
	}
	byDay := map[string]*trendRow{}
	var order []string
	for rows.Next() {
		var day time.Time
		var active, expired int64
		if err := rows.Scan(&day, &active, &expired); err != nil {
			rows.Close()
			m.internalError(w, "trends subscribers scan", err)
			return
		}
		key := day.Format("2006-01-02")
		byDay[key] = &trendRow{Date: key, Active: active, Expired: expired}
		order = append(order, key)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		m.internalError(w, "trends subscribers rows", err)
		return
	}

	onlineRows, err := m.db.Query(ctx,
		`SELECT time_bucket('1 day', at)::date AS day, round(avg(online))::int
		   FROM online_samples
		  WHERE at >= $1::timestamptz AND at < $2::timestamptz + interval '1 day'
		  GROUP BY day ORDER BY day`, from, to)
	if err != nil {
		m.internalError(w, "trends online", err)
		return
	}
	for onlineRows.Next() {
		var day time.Time
		var online int64
		if err := onlineRows.Scan(&day, &online); err != nil {
			onlineRows.Close()
			m.internalError(w, "trends online scan", err)
			return
		}
		key := day.Format("2006-01-02")
		if row, ok := byDay[key]; ok {
			row.Online = online
		}
	}
	onlineRows.Close()
	if err := onlineRows.Err(); err != nil {
		m.internalError(w, "trends online rows", err)
		return
	}

	out := make([]trendRow, 0, len(order))
	for _, k := range order {
		out = append(out, *byDay[k])
	}
	httpapi.JSON(w, http.StatusOK, map[string]any{"rows": out})
}

type dailyTrafficRow struct {
	Date      string `json:"date"`
	DownBytes int64  `json:"down_bytes"`
	UpBytes   int64  `json:"up_bytes"`
}

// dailyTrafficHandler answers GET /api/v1/reports/daily-traffic?from=&to=:
// network-wide (or scoped) upload/download per day, straight off usage_daily.
func (m *Module) dailyTrafficHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	scope := auth.ScopeFilter(ctx)
	from, to := parseRange(r)
	if from.Equal(epoch) {
		from = to.AddDate(0, 0, -30)
	}

	q := `SELECT u.day::date, sum(u.down_bytes)::bigint, sum(u.up_bytes)::bigint
	        FROM usage_daily u`
	args := []any{from, to}
	where := ` WHERE u.day >= $1 AND u.day <= $2`
	if scope != nil {
		q += ` JOIN subscribers s ON s.id = u.subscriber_id`
		where += ` AND s.owner_manager_id = $3::uuid`
		args = append(args, scope.ManagerID)
	}
	q += where + ` GROUP BY u.day ORDER BY u.day`

	rows, err := m.db.Query(ctx, q, args...)
	if err != nil {
		m.internalError(w, "daily traffic", err)
		return
	}
	defer rows.Close()
	out := []dailyTrafficRow{}
	for rows.Next() {
		var day time.Time
		var dr dailyTrafficRow
		if err := rows.Scan(&day, &dr.DownBytes, &dr.UpBytes); err != nil {
			m.internalError(w, "daily traffic scan", err)
			return
		}
		dr.Date = day.Format("2006-01-02")
		out = append(out, dr)
	}
	if err := rows.Err(); err != nil {
		m.internalError(w, "daily traffic rows", err)
		return
	}
	httpapi.JSON(w, http.StatusOK, map[string]any{"rows": out})
}

type top10Row struct {
	SubscriberID string `json:"subscriber_id"`
	Username     string `json:"username"`
	Bytes        int64  `json:"bytes"`
}

type top10Response struct {
	DownloadToday []top10Row `json:"download_today"`
	UploadToday   []top10Row `json:"upload_today"`
	DownloadMonth []top10Row `json:"download_month"`
	UploadMonth   []top10Row `json:"upload_month"`
}

// top10Handler answers GET /api/v1/reports/top10: the four fixed-window
// leaderboards (download/upload x today/month), all off usage_daily.
//
// "Today"/"month" are calendar boundaries in UTC, not the ISP's Asia/Baghdad
// timezone dashboard_api.go's revenueToday uses for its own (differently
// sourced) figure — deliberately, not an oversight: usage_daily's `day`
// bucket (migration 0132's time_bucket) is anchored to UTC midnight
// regardless of session timezone (confirmed against a live instance: a
// point's bucket reflects its UTC calendar day even though this database's
// own session TimeZone GUC is Asia/Baghdad). A day-granularity rollup that
// already exists cannot be re-sliced to a different timezone's midnight
// after the fact, so pretending Baghdad-local "today" here would silently
// misattribute up to 3 hours of traffic (Baghdad is UTC+3) at every day
// boundary. Using the bucket's own anchor is the only version of "today"
// this data can answer exactly.
func (m *Module) top10Handler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	scope := auth.ScopeFilter(ctx)

	one := func(col string, monthOnly bool) ([]top10Row, error) {
		dayFilter := `u.day = date_trunc('day', now() AT TIME ZONE 'UTC') AT TIME ZONE 'UTC'`
		if monthOnly {
			dayFilter = `u.day >= date_trunc('month', now() AT TIME ZONE 'UTC') AT TIME ZONE 'UTC'`
		}
		q := `SELECT u.subscriber_id::text, COALESCE(s.username::text,''), sum(u.` + col + `)::bigint AS b
		        FROM usage_daily u
		        JOIN subscribers s ON s.id = u.subscriber_id
		       WHERE u.subscriber_id IS NOT NULL AND ` + dayFilter
		args := []any{}
		if scope != nil {
			q += ` AND s.owner_manager_id = $1::uuid`
			args = append(args, scope.ManagerID)
		}
		q += ` GROUP BY u.subscriber_id, s.username ORDER BY b DESC LIMIT 10`
		rows, err := m.db.Query(ctx, q, args...)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		out := []top10Row{}
		for rows.Next() {
			var t top10Row
			if err := rows.Scan(&t.SubscriberID, &t.Username, &t.Bytes); err != nil {
				return nil, err
			}
			out = append(out, t)
		}
		return out, rows.Err()
	}

	var resp top10Response
	var err error
	if resp.DownloadToday, err = one("down_bytes", false); err != nil {
		m.internalError(w, "top10 download today", err)
		return
	}
	if resp.UploadToday, err = one("up_bytes", false); err != nil {
		m.internalError(w, "top10 upload today", err)
		return
	}
	if resp.DownloadMonth, err = one("down_bytes", true); err != nil {
		m.internalError(w, "top10 download month", err)
		return
	}
	if resp.UploadMonth, err = one("up_bytes", true); err != nil {
		m.internalError(w, "top10 upload month", err)
		return
	}
	httpapi.JSON(w, http.StatusOK, resp)
}
