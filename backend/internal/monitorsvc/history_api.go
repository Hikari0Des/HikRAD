package monitorsvc

// Per-NAS and per-device status pages (contract C5 / FR-34 / FR-60). Both share
// one response shape (E renders NAS and device history identically): the current
// reachability state, the probe series (ICMP latency/loss + SNMP cpu/mem/uptime)
// over a window, and a downtime log derived from the ICMP up/down edges. State is
// recomputed from the health_probes table here (the API runs in hikrad-api, not
// the monitor process, so it can't read the engine's in-memory state) using the
// same N-miss rule as the live state machine.

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/hikrad/hikrad/internal/httpapi"
	"github.com/jackc/pgx/v5/pgconn"
)

type probeSample struct {
	At        time.Time `json:"at"`
	Kind      string    `json:"kind"`
	LatencyMS *float64  `json:"latency_ms,omitempty"`
	Loss      *float64  `json:"loss,omitempty"`
	CPU       *float64  `json:"cpu,omitempty"`
	Mem       *float64  `json:"mem,omitempty"`
	Uptime    *int64    `json:"uptime,omitempty"`
	OK        bool      `json:"ok"`
}

type downtimeWindow struct {
	From    time.Time `json:"from"`
	To      *time.Time `json:"to"` // null = still down
	Seconds int64     `json:"seconds"`
}

type probeHistoryResponse struct {
	Status   string           `json:"status"`
	Series   []probeSample    `json:"series"`
	Downtime []downtimeWindow `json:"downtime"`
}

func nasProbeHistory(w http.ResponseWriter, r *http.Request) {
	probeHistory(w, r, "nas_id", chi.URLParam(r, "id"))
}

func deviceProbeHistory(w http.ResponseWriter, r *http.Request) {
	probeHistory(w, r, "device_id", chi.URLParam(r, "id"))
}

func probeHistory(w http.ResponseWriter, r *http.Request, col, id string) {
	ctx := r.Context()
	to := parseTime(r.URL.Query().Get("to"), time.Now().UTC())
	from := parseTime(r.URL.Query().Get("from"), to.Add(-24*time.Hour))

	series, err := loadProbeSeries(ctx, col, id, from, to)
	if err != nil {
		internalErr(w, "probe series", err)
		return
	}
	resp := probeHistoryResponse{
		Status:   statusFromProbes(ctx, col, id),
		Series:   series,
		Downtime: downtimeFromSeries(series, to),
	}
	if resp.Series == nil {
		resp.Series = []probeSample{}
	}
	if resp.Downtime == nil {
		resp.Downtime = []downtimeWindow{}
	}
	httpapi.JSON(w, http.StatusOK, resp)
}

// loadProbeSeries returns all probes (both kinds) for a target in [from,to) asc.
func loadProbeSeries(ctx context.Context, col, id string, from, to time.Time) ([]probeSample, error) {
	rows, err := pkgDB.Query(ctx,
		`SELECT at, kind, latency_ms, loss, cpu, mem, uptime, ok
		   FROM health_probes
		  WHERE `+col+` = $1::uuid AND at >= $2 AND at < $3
		  ORDER BY at`, id, from, to)
	if err != nil {
		if isUndefinedTable(err) {
			return []probeSample{}, nil
		}
		return nil, err
	}
	defer rows.Close()
	var out []probeSample
	for rows.Next() {
		var s probeSample
		if err := rows.Scan(&s.At, &s.Kind, &s.LatencyMS, &s.Loss, &s.CPU, &s.Mem, &s.Uptime, &s.OK); err != nil {
			return nil, err
		}
		s.At = s.At.UTC()
		out = append(out, s)
	}
	return out, rows.Err()
}

// statusFromProbes recomputes up/down/unknown from the most recent ICMP probes,
// applying the same downThreshold rule as the live state machine. This lets the
// read API report state without sharing memory with the monitor process.
func statusFromProbes(ctx context.Context, col, id string) string {
	rows, err := pkgDB.Query(ctx,
		`SELECT ok FROM health_probes
		  WHERE `+col+` = $1::uuid AND kind = 'icmp'
		  ORDER BY at DESC LIMIT $2`, id, downThreshold)
	if err != nil {
		return string(statusUnknown)
	}
	defer rows.Close()
	var oks []bool
	for rows.Next() {
		var ok bool
		if err := rows.Scan(&ok); err != nil {
			return string(statusUnknown)
		}
		oks = append(oks, ok)
	}
	if len(oks) == 0 {
		return string(statusUnknown)
	}
	if oks[0] {
		return string(statusUp) // latest probe succeeded
	}
	// Latest failed: down only once the miss streak has reached the threshold.
	if len(oks) >= downThreshold {
		allDown := true
		for _, ok := range oks {
			if ok {
				allDown = false
				break
			}
		}
		if allDown {
			return string(statusDown)
		}
	}
	return string(statusUnknown) // failing but not yet past the threshold
}

// downtimeFromSeries walks the ICMP samples and folds them into down windows via
// the state machine, so the log matches exactly when alerts fired. An open
// window (still down at the end) has a nil To.
func downtimeFromSeries(series []probeSample, end time.Time) []downtimeWindow {
	var st targetState
	var out []downtimeWindow
	var openFrom *time.Time
	for _, s := range series {
		if s.Kind != "icmp" {
			continue
		}
		at := s.At
		tr := st.observe(s.OK)
		if tr.toDown {
			f := at
			openFrom = &f
		}
		if tr.toUp && openFrom != nil {
			to := at
			out = append(out, downtimeWindow{From: *openFrom, To: &to, Seconds: int64(to.Sub(*openFrom).Seconds())})
			openFrom = nil
		}
	}
	if openFrom != nil {
		out = append(out, downtimeWindow{From: *openFrom, To: nil, Seconds: int64(end.Sub(*openFrom).Seconds())})
	}
	return out
}

// deviceStatus / nasStatus are thin wrappers used by the CRUD + dashboard code.
func deviceStatus(ctx context.Context, id string) string { return statusFromProbes(ctx, "device_id", id) }
func nasStatus(ctx context.Context, id string) string    { return statusFromProbes(ctx, "nas_id", id) }

func parseTime(v string, def time.Time) time.Time {
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

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505"
	}
	return false
}

// isUndefinedTable mirrors accounting's helper for the (rare) window where a
// monitoring migration hasn't applied yet — a read degrades to empty, not 500.
func isUndefinedTable(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "42P01" || pgErr.Code == "42703"
	}
	return false
}
