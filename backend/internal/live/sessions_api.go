package live

// Session history + usage-graph REST (contract C7-C, FR-31/FR-33):
//   GET /api/v1/sessions?subscriber_id=          — paginated history
//   GET /api/v1/usage/subscriber/{id}?granularity=daily|monthly&from&to
// Both apply manager scope (FR-27.2). Usage reads the usage_daily rollup (all
// services — graphs include hotspot, FR-58.3) with real-time aggregation so the
// most recent day is not missing.

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/hikrad/hikrad/internal/auth"
	"github.com/hikrad/hikrad/internal/httpapi"
)

type sessionView struct {
	ID            string     `json:"id"`
	NASID         string     `json:"nas_id"`
	AcctSessionID string     `json:"acct_session_id"`
	SubscriberID  string     `json:"subscriber_id"`
	Username      string     `json:"username"`
	IP            string     `json:"ip"`
	MAC           string     `json:"mac"`
	StartedAt     *time.Time `json:"started_at"`
	StoppedAt     *time.Time `json:"stopped_at"`
	LastInterimAt *time.Time `json:"last_interim_at"`
	TerminateCause string    `json:"terminate_cause"`
	BytesIn       int64      `json:"bytes_in"`
	BytesOut      int64      `json:"bytes_out"`
	Stale         bool       `json:"stale"`
	Reaped        bool       `json:"reaped"`
	Service       string     `json:"service"`
	// NASServiceID / ServiceName name WHICH instance the session ran on (FR-62).
	// "hotspot" alone does not tell an operator which of three zones a user is
	// on. Empty when the session could not be attributed to an instance — which
	// is recorded, never dropped (M2).
	NASServiceID string `json:"nas_service_id"`
	ServiceName  string `json:"service_name"`
}

func (m *Module) listSessions(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	page, err := httpapi.ParsePage(r)
	if err != nil {
		httpapi.Error(w, http.StatusBadRequest, "invalid_pagination", err.Error())
		return
	}
	var cursorTS *time.Time
	var cursorID *string
	if len(page.Cursor) == 2 {
		t, terr := time.Parse(time.RFC3339Nano, page.Cursor[0])
		if terr != nil {
			httpapi.Error(w, http.StatusBadRequest, "invalid_pagination", "malformed cursor")
			return
		}
		cursorTS, cursorID = &t, &page.Cursor[1]
	} else if page.Cursor != nil {
		httpapi.Error(w, http.StatusBadRequest, "invalid_pagination", "malformed cursor")
		return
	}

	var subFilter *string
	if v := r.URL.Query().Get("subscriber_id"); v != "" {
		subFilter = &v
	}
	allowed, unscoped := allowedSubscribers(ctx, pkgDB, auth.ScopeFilter(ctx))
	if allowed == nil {
		allowed = []string{}
	}

	rows, err := pkgDB.Query(ctx,
		`SELECT s.id::text, COALESCE(s.nas_id::text,''), s.acct_session_id,
		        COALESCE(s.subscriber_id::text,''), s.username, COALESCE(host(s.ip),''), s.mac,
		        s.started_at, s.stopped_at, s.last_interim_at, s.terminate_cause,
		        s.bytes_in, s.bytes_out, s.stale, s.reaped, s.service,
		        COALESCE(s.nas_service_id::text,''),
		        -- The operator's label, else the router's own server name (FR-62).
		        COALESCE(NULLIF(ns.label,''), ns.ros_server_name, '') AS service_name,
		        COALESCE(s.started_at, s.created_at) AS sort_ts
		   FROM sessions s
		   LEFT JOIN nas_services ns ON ns.id = s.nas_service_id
		  WHERE ($1::uuid IS NULL OR s.subscriber_id = $1::uuid)
		    AND ($2::boolean OR s.subscriber_id = ANY($3::uuid[]))
		    AND ($4::timestamptz IS NULL
		         OR (COALESCE(s.started_at, s.created_at), s.id) < ($4::timestamptz, $5::uuid))
		  ORDER BY sort_ts DESC, s.id DESC
		  LIMIT $6`,
		subFilter, unscoped, allowed, cursorTS, cursorID, page.Limit+1)
	if err != nil {
		m.internal(w, "sessions query", err)
		return
	}
	defer rows.Close()

	items := make([]sessionView, 0, page.Limit)
	var lastSort time.Time
	for rows.Next() {
		var s sessionView
		var sortTS time.Time
		if err := rows.Scan(&s.ID, &s.NASID, &s.AcctSessionID, &s.SubscriberID, &s.Username, &s.IP, &s.MAC,
			&s.StartedAt, &s.StoppedAt, &s.LastInterimAt, &s.TerminateCause,
			&s.BytesIn, &s.BytesOut, &s.Stale, &s.Reaped, &s.Service,
			&s.NASServiceID, &s.ServiceName, &sortTS); err != nil {
			m.internal(w, "sessions scan", err)
			return
		}
		s.StartedAt = utcp(s.StartedAt)
		s.StoppedAt = utcp(s.StoppedAt)
		s.LastInterimAt = utcp(s.LastInterimAt)
		lastSort = sortTS
		items = append(items, s)
	}
	if rows.Err() != nil {
		m.internal(w, "sessions rows", rows.Err())
		return
	}
	next := ""
	if len(items) > page.Limit {
		items = items[:page.Limit]
		last := items[len(items)-1]
		next = httpapi.EncodeCursor(lastSort.UTC().Format(time.RFC3339Nano), last.ID)
	}
	httpapi.JSON(w, http.StatusOK, httpapi.NewListResponse(items, next))
}

func (m *Module) usageBySubscriber(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")

	// Scope: a scoped manager may only read their own subscribers' usage.
	if allowed, unscoped := allowedSubscribers(ctx, pkgDB, auth.ScopeFilter(ctx)); !unscoped {
		if !contains(allowed, id) {
			httpapi.Error(w, http.StatusForbidden, "forbidden", "not permitted for this subscriber")
			return
		}
	}

	monthly := r.URL.Query().Get("granularity") == "monthly"
	defWindow := 30 * 24 * time.Hour
	if monthly {
		defWindow = 365 * 24 * time.Hour
	}
	to := parseTimeParam(r.URL.Query().Get("to"), time.Now().UTC())
	from := parseTimeParam(r.URL.Query().Get("from"), to.Add(-defWindow))

	out, err := UsageForSubscriber(ctx, pkgDB, id, monthly, from, to)
	if err != nil {
		m.internal(w, "usage query", err)
		return
	}
	httpapi.JSON(w, http.StatusOK, out)
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

func utcp(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}

func contains(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}
