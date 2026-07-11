package monitorsvc

// Dashboard API (FR-32, contract C5). One call answers "is my network OK, is my
// business OK": online-now (≤ 2 s fresh, straight off the live hash), a 24 h
// online sparkline (downsampled from the per-minute online_samples), subscriber
// tiles, today's revenue (D's revenue_daily view, read-only), NAS reachability
// cards (from probe history), the RADIUS request rate and the pipeline invariant.
// Every cross-domain read degrades to a zero/empty value if its source table
// isn't present yet (parallel agents), never a 500.

import (
	"context"
	"net/http"
	"time"

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
	OnlineNow         int64          `json:"online_now"`
	Online24hSpark    []sparkPoint   `json:"online_24h_sparkline"`
	Subs              subTiles       `json:"subs"`
	RevenueTodayIQD   int64          `json:"revenue_today_iqd"`
	NASCards          []nasCard      `json:"nas_cards"`
	RadiusRPS         float64        `json:"radius_rps"`
	Pipeline          pipelineTile   `json:"pipeline"`
}

func handleDashboard(w http.ResponseWriter, r *http.Request) {
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
func revenueToday(ctx context.Context) int64 {
	if pkgDB == nil {
		return 0
	}
	for _, col := range []string{"revenue_iqd", "amount_iqd", "total_iqd", "revenue", "amount"} {
		var v int64
		err := pkgDB.QueryRow(ctx,
			`SELECT COALESCE(SUM(`+col+`),0) FROM revenue_daily
			  WHERE day = (now() AT TIME ZONE 'Asia/Baghdad')::date`).Scan(&v)
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
