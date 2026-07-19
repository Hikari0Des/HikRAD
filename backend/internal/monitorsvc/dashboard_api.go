package monitorsvc

// Dashboard API (FR-32, contract C5; v2-10 FR-89/90, contract C3). One call
// answers "is my network OK, is my business OK": online-now (≤ 2 s fresh,
// straight off the live hash), a 24 h online sparkline (downsampled from the
// per-minute online_samples), subscriber tiles, today's revenue (D's
// revenue_daily view, read-only), NAS reachability cards (from probe
// history), the RADIUS request rate, the pipeline invariant, and (v2-10) a
// manager's own balance, pending payment-ticket count, and an alerts feed.
// Every cross-domain read degrades to a zero/empty value if its source table
// isn't present yet (parallel agents), never a 500.
//
// Two request shapes, frozen by C3:
//   - No ?widgets= at all: the exact pre-v2-10 behavior — requires
//     monitoring.view (including its audit-on-denial, via the route's
//     dashboardAccess middleware), every original field always present,
//     byte-for-byte unchanged (gate item 8).
//   - ?widgets=<comma-separated ids>: the new per-widget path — any
//     authenticated manager may call it, a forbidden or unknown id is
//     dropped from the response rather than erroring (FR-89.3), and only
//     the data the surviving ids need is computed at all.

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/hikrad/hikrad/internal/auth"
	"github.com/hikrad/hikrad/internal/httpapi"
	"github.com/hikrad/hikrad/internal/live/livestate"
)

type sparkPoint struct {
	T      time.Time `json:"t"`
	Online int       `json:"online"`
}

type subTiles struct {
	Active     int64 `json:"active"`
	Expired    int64 `json:"expired"`
	Expiring7d int64 `json:"expiring_7d"`
}

type nasCard struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Status    string   `json:"status"`
	LatencyMS *float64 `json:"latency_ms"`
	DowntimeS *int64   `json:"downtime_s,omitempty"`
}

type pipelineTile struct {
	InvariantOK bool  `json:"invariant_ok"`
	Depth       int64 `json:"depth"`
}

type dashboardResponse struct {
	OnlineNow       int64        `json:"online_now"`
	Online24hSpark  []sparkPoint `json:"online_24h_sparkline"`
	Subs            subTiles     `json:"subs"`
	RevenueTodayIQD int64        `json:"revenue_today_iqd"`
	NASCards        []nasCard    `json:"nas_cards"`
	RadiusRPS       float64      `json:"radius_rps"`
	Pipeline        pipelineTile `json:"pipeline"`
}

// dashboardAccess is C3's per-request branch. The legacy (no ?widgets=) call
// keeps the exact old auth.Require(PermView) gate — including its
// audit-on-denial — so gate item 8's byte-for-byte promise covers observable
// side effects, not just the response body. The new ?widgets= path only
// authenticates; per-widget authorization happens inside the handler (C1).
func dashboardAccess(next http.Handler) http.Handler {
	legacy := auth.Require(PermView)(next)
	filtered := auth.Require("")(next)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Has("widgets") {
			filtered.ServeHTTP(w, r)
			return
		}
		legacy.ServeHTTP(w, r)
	})
}

func handleDashboard(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Has("widgets") {
		handleDashboardFiltered(w, r)
		return
	}
	handleDashboardLegacy(w, r)
}

// handleDashboardLegacy is the frozen pre-v2-10 response — unchanged.
func handleDashboardLegacy(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	resp := dashboardResponse{
		OnlineNow:       onlineNow(ctx),
		Online24hSpark:  onlineSparkline(ctx),
		Subs:            subscriberTiles(ctx),
		RevenueTodayIQD: revenueToday(ctx),
		NASCards:        nasCards(ctx),
		RadiusRPS:       freeRADIUSHealth(ctx).ReqRate,
	}
	snap := fetchAcctCounters(ctx)
	if snap != nil {
		if v, ok := snap["invariant_ok"].(bool); ok {
			resp.Pipeline.InvariantOK = v
		}
		if v, ok := toInt64(snap["in_queue"]); ok {
			resp.Pipeline.Depth = v
		}
	}
	if resp.Online24hSpark == nil {
		resp.Online24hSpark = []sparkPoint{}
	}
	if resp.NASCards == nil {
		resp.NASCards = []nasCard{}
	}
	httpapi.JSON(w, http.StatusOK, resp)
}

// handleDashboardFiltered is FR-89.3/C3's new path: computes and returns
// only the keys the (permission-filtered) requested widget ids need.
func handleDashboardFiltered(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	mgr, _ := auth.ManagerFrom(ctx)

	var requested []string
	if raw := r.URL.Query().Get("widgets"); raw != "" {
		requested = strings.Split(raw, ",")
	}
	ids := filterDashboardWidgets(mgr, requested)

	out := map[string]any{}
	if containsWidget(ids, "online-now") {
		out["online_now"] = onlineNow(ctx)
		spark := onlineSparkline(ctx)
		if spark == nil {
			spark = []sparkPoint{}
		}
		out["online_24h_sparkline"] = spark
	}
	if containsWidget(ids, "revenue-today") {
		out["revenue_today_iqd"] = revenueToday(ctx)
	}
	if containsWidget(ids, "radius-rps") {
		out["radius_rps"] = freeRADIUSHealth(ctx).ReqRate
	}
	if needsSubsQuery(ids) {
		out["subs"] = subscriberTiles(ctx)
	}
	if containsWidget(ids, "pipeline-health") {
		p := pipelineTile{}
		if snap := fetchAcctCounters(ctx); snap != nil {
			if v, ok := snap["invariant_ok"].(bool); ok {
				p.InvariantOK = v
			}
			if v, ok := toInt64(snap["in_queue"]); ok {
				p.Depth = v
			}
		}
		out["pipeline"] = p
	}
	if containsWidget(ids, "nas-health") {
		cards := nasCards(ctx)
		if cards == nil {
			cards = []nasCard{}
		}
		out["nas_cards"] = cards
	}
	if containsWidget(ids, "my-balance") && mgr != nil {
		out["my_balance"] = myBalances(ctx, mgr.ID)
	}
	if containsWidget(ids, "pending-payment-tickets") && mgr != nil {
		out["pending_payment_tickets"] = pendingPaymentTickets(ctx)
	}
	if containsWidget(ids, "alerts-feed") {
		out["alerts_feed"] = alertsFeed(ctx)
	}
	if containsWidget(ids, "top-usage-subscribers") {
		out["top_usage_subscribers"] = topUsageSubscribers(ctx)
	}
	if containsWidget(ids, "top-session-subscribers") {
		out["top_session_subscribers"] = topSessionSubscribers(ctx)
	}
	httpapi.JSON(w, http.StatusOK, out)
}

// onlineNow is the live-session count straight off the Redis hash (≤ 2 s fresh).
func onlineNow(ctx context.Context) int64 {
	if pkgRDB == nil {
		return 0
	}
	c, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	n, err := pkgRDB.HLen(c, livestate.HashKey).Result()
	if err != nil {
		return 0
	}
	return n
}

// onlineSparkline downsamples the per-minute samples into 15-minute buckets over
// the last 24 h (contract: "downsampled from live-count samples you record each
// minute"). Uses the bucket average so a transient spike doesn't dominate.
func onlineSparkline(ctx context.Context) []sparkPoint {
	if pkgDB == nil {
		return nil
	}
	rows, err := pkgDB.Query(ctx,
		`SELECT time_bucket('15 minutes', at) AS b, round(avg(online))::int
		   FROM online_samples
		  WHERE at >= now() - interval '24 hours'
		  GROUP BY b ORDER BY b`)
	if err != nil {
		return nil // view/table missing or empty → empty sparkline
	}
	defer rows.Close()
	var out []sparkPoint
	for rows.Next() {
		var p sparkPoint
		if err := rows.Scan(&p.T, &p.Online); err != nil {
			return out
		}
		p.T = p.T.UTC()
		out = append(out, p)
	}
	return out
}

func subscriberTiles(ctx context.Context) subTiles {
	var t subTiles
	if pkgDB == nil {
		return t
	}
	// Active, expired, and expiring within 7 days. Expiry is evaluated against
	// now() so the tiles agree with the auth-time view even between sweeps.
	err := pkgDB.QueryRow(ctx,
		`SELECT
		   count(*) FILTER (WHERE status = 'active'),
		   count(*) FILTER (WHERE status = 'expired' OR (expires_at IS NOT NULL AND expires_at < now())),
		   count(*) FILTER (WHERE status = 'active' AND expires_at IS NOT NULL
		                     AND expires_at >= now() AND expires_at < now() + interval '7 days')
		 FROM subscribers`).Scan(&t.Active, &t.Expired, &t.Expiring7d)
	if err != nil {
		return subTiles{}
	}
	return t
}

// revenueToday reads today's (Asia/Baghdad) revenue from D's revenue_daily view.
// The view's amount column name isn't frozen in C5, so a small ordered set of
// likely names is tried; any undefined-table/column degrades to 0 rather than
// failing the whole dashboard while D's migration is still landing.
//
// v2 phase 4: fixed a pre-existing bug found while making this view
// currency-aware — the WHERE clause filtered on a column named `day`, but
// revenue_daily has always named it `date` (migration 0201), so every probe
// in the candidate loop failed identically and this tile has always silently
// reported 0. Also now IQD-scoped (FR-70.2): the view is grouped by currency
// too (migration 0537), and a bare SUM would blend currencies — see
// docs/ops/known-issues.md.
func revenueToday(ctx context.Context) int64 {
	if pkgDB == nil {
		return 0
	}
	for _, col := range []string{"revenue_iqd", "amount_iqd", "total_iqd", "revenue", "amount"} {
		var v int64
		err := pkgDB.QueryRow(ctx,
			`SELECT COALESCE(SUM(`+col+`),0) FROM revenue_daily
			  WHERE date = (now() AT TIME ZONE 'Asia/Baghdad')::date AND currency = 'IQD'`).Scan(&v)
		if err == nil {
			return v
		}
		if !isUndefinedTable(err) {
			return 0 // real error (not a missing column) — stop probing
		}
	}
	return 0
}

func nasCards(ctx context.Context) []nasCard {
	if pkgDB == nil {
		return nil
	}
	rows, err := pkgDB.Query(ctx, `SELECT id::text, name FROM nas WHERE enabled ORDER BY name`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	type nasRow struct{ id, name string }
	var nasList []nasRow
	for rows.Next() {
		var n nasRow
		if err := rows.Scan(&n.id, &n.name); err != nil {
			return nil
		}
		nasList = append(nasList, n)
	}
	if rows.Err() != nil {
		return nil
	}
	out := make([]nasCard, 0, len(nasList))
	for _, n := range nasList {
		card := nasCard{ID: n.id, Name: n.name, Status: statusFromProbes(ctx, "nas_id", n.id)}
		card.LatencyMS = latestLatency(ctx, "nas_id", n.id)
		if card.Status == string(statusDown) {
			if s := openDowntimeSeconds(ctx, "nas_id", n.id); s > 0 {
				card.DowntimeS = &s
			}
		}
		out = append(out, card)
	}
	return out
}

// latestLatency returns the most recent successful ICMP RTT for a target.
func latestLatency(ctx context.Context, col, id string) *float64 {
	var ms *float64
	err := pkgDB.QueryRow(ctx,
		`SELECT latency_ms FROM health_probes
		  WHERE `+col+` = $1::uuid AND kind='icmp' AND ok
		  ORDER BY at DESC LIMIT 1`, id).Scan(&ms)
	if err != nil {
		return nil
	}
	return ms
}

// openDowntimeSeconds returns seconds since the last successful ICMP probe of a
// currently-down target (the ongoing outage length).
func openDowntimeSeconds(ctx context.Context, col, id string) int64 {
	var last *time.Time
	err := pkgDB.QueryRow(ctx,
		`SELECT max(at) FROM health_probes
		  WHERE `+col+` = $1::uuid AND kind='icmp' AND ok`, id).Scan(&last)
	if err != nil || last == nil {
		return 0
	}
	return int64(time.Since(*last).Seconds())
}

// --- v2-10 new widgets (C1) --------------------------------------------------

type balanceView struct {
	Currency string `json:"currency"`
	Balance  int64  `json:"balance"`
}

// myBalances reads the caller's own per-currency balances directly (same
// "cross-domain reads are raw SQL, never a cross-package Go import" pattern
// revenueToday/nasCards already establish in this file — C1's my-balance row).
func myBalances(ctx context.Context, managerID string) []balanceView {
	out := []balanceView{}
	if pkgDB == nil {
		return out
	}
	rows, err := pkgDB.Query(ctx,
		`SELECT currency, balance FROM manager_balances WHERE manager_id = $1::uuid ORDER BY currency`, managerID)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var b balanceView
		if err := rows.Scan(&b.Currency, &b.Balance); err != nil {
			return out
		}
		out = append(out, b)
	}
	return out
}

// pendingPaymentTickets counts payment_tickets in state 'pending' — unscoped
// (admin/operator) callers see the whole business, a scoped manager sees only
// tickets on subscribers they own, same ScopeFilter posture as every other
// scoped list (FR-27.2). ctx must carry the resolved auth.Manager (ScopeFilter
// reads it from context).
func pendingPaymentTickets(ctx context.Context) int64 {
	if pkgDB == nil {
		return 0
	}
	scope := auth.ScopeFilter(ctx)
	var n int64
	var err error
	if scope != nil {
		err = pkgDB.QueryRow(ctx,
			`SELECT count(*) FROM payment_tickets pt
			   JOIN subscribers s ON s.id = pt.subscriber_id
			  WHERE pt.state = 'pending' AND s.owner_manager_id = $1::uuid`,
			scope.ManagerID).Scan(&n)
	} else {
		err = pkgDB.QueryRow(ctx, `SELECT count(*) FROM payment_tickets WHERE state = 'pending'`).Scan(&n)
	}
	if err != nil {
		return 0
	}
	return n
}

type topUsageItem struct {
	SubscriberID string `json:"subscriber_id"`
	Username     string `json:"username"`
	Service      string `json:"service"`
	Bytes        int64  `json:"bytes"`
}

// topUsageSubscribers is the item-2 "who used the most data" leaderboard:
// last 7 days from usage_daily, per subscriber per service (so a heavy
// hotspot user and a heavy pppoe user rank independently, matching how
// rates/quotas are service-scoped since FR-61). Same degrade-to-empty
// posture as every other cross-domain read here.
func topUsageSubscribers(ctx context.Context) []topUsageItem {
	out := []topUsageItem{}
	if pkgDB == nil {
		return out
	}
	rows, err := pkgDB.Query(ctx,
		`SELECT u.subscriber_id::text, COALESCE(s.username, ''), u.service,
		        SUM(u.down_bytes + u.up_bytes)::bigint AS bytes
		   FROM usage_daily u
		   LEFT JOIN subscribers s ON s.id = u.subscriber_id
		  WHERE u.day >= now() - interval '7 days' AND u.subscriber_id IS NOT NULL
		  GROUP BY u.subscriber_id, s.username, u.service
		  ORDER BY bytes DESC
		  LIMIT 8`)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var it topUsageItem
		if err := rows.Scan(&it.SubscriberID, &it.Username, &it.Service, &it.Bytes); err != nil {
			return out
		}
		out = append(out, it)
	}
	return out
}

type topSessionItem struct {
	SubscriberID string `json:"subscriber_id"`
	Username     string `json:"username"`
	OpenSessions int64  `json:"open_sessions"`
}

// topSessionSubscribers ranks subscribers by currently-open session count —
// the "who has the most simultaneous logins" view (item 2).
func topSessionSubscribers(ctx context.Context) []topSessionItem {
	out := []topSessionItem{}
	if pkgDB == nil {
		return out
	}
	rows, err := pkgDB.Query(ctx,
		`SELECT subscriber_id::text, MAX(username), count(*)::bigint AS n
		   FROM sessions
		  WHERE stopped_at IS NULL AND subscriber_id IS NOT NULL
		  GROUP BY subscriber_id
		  ORDER BY n DESC
		  LIMIT 8`)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var it topSessionItem
		if err := rows.Scan(&it.SubscriberID, &it.Username, &it.OpenSessions); err != nil {
			return out
		}
		out = append(out, it)
	}
	return out
}

type alertFeedItem struct {
	ID      string    `json:"id"`
	At      time.Time `json:"at"`
	Type    string    `json:"type"`
	Summary string    `json:"summary"`
}

// alertsFeed returns the most recent alert events — this module already owns
// alert_events, so this is an in-package query, not a cross-domain read.
func alertsFeed(ctx context.Context) []alertFeedItem {
	out := []alertFeedItem{}
	if pkgDB == nil {
		return out
	}
	rows, err := pkgDB.Query(ctx,
		`SELECT id::text, at, type, summary FROM alert_events ORDER BY at DESC LIMIT 10`)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var it alertFeedItem
		if err := rows.Scan(&it.ID, &it.At, &it.Type, &it.Summary); err != nil {
			return out
		}
		it.At = it.At.UTC()
		out = append(out, it)
	}
	return out
}
