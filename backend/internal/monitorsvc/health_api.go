package monitorsvc

// System self-monitoring (FR-35, surfacing the FR-40 invariant). GET
// /api/v1/health composes: FreeRADIUS (up + request/reject rate from the
// radius:decisions stream), the API/DB/Redis liveness, the accounting queue
// (depth + drain rate + the M2 conservation invariant + B's enforcement-failure
// counter), and disk usage per configured path. It reads only durable/derivable
// sources — the acct counters endpoint (with a pipeline_counters fallback), the
// decisions stream, Redis handoff keys the monitor writes — so it never imports
// internal/radius or internal/accounting.

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/hikrad/hikrad/internal/httpapi"
	"github.com/redis/go-redis/v9"
)

// Redis handoff keys the monitor self-check writes for the health API (only the
// monitor can do the Status-Server probe / rate sampling).
const (
	keyFreeRADIUSUp = "health:freeradius:up"
	keyAcctDrain    = "health:acct:drain_rate"
	// enforcement-failure counter (frozen key name B exposes; read directly to
	// avoid importing internal/radius).
	keyEnforceFailures = "enforce:failures"
	decisionStream     = "radius:decisions"
)

type componentHealth struct {
	Up bool `json:"up"`
}

type freeradiusHealth struct {
	Up         bool    `json:"up"`
	ReqRate    float64 `json:"req_rate"`    // Access-Requests/sec over the last minute
	RejectRate float64 `json:"reject_rate"` // fraction rejected [0,1] over the last minute
}

type queueHealth struct {
	Depth               int64          `json:"depth"`
	DrainRate           float64        `json:"drain_rate"` // records persisted/sec
	InvariantOK         bool           `json:"invariant_ok"`
	EnforcementFailures int64          `json:"enforcement_failures"`
	Counters            map[string]any `json:"counters"`
}

type diskUsage struct {
	Path        string  `json:"path"`
	TotalBytes  uint64  `json:"total_bytes"`
	UsedBytes   uint64  `json:"used_bytes"`
	FreeBytes   uint64  `json:"free_bytes"`
	UsedPercent float64 `json:"used_percent"`
}

type healthResponse struct {
	FreeRADIUS freeradiusHealth `json:"freeradius"`
	API        componentHealth  `json:"api"`
	DB         componentHealth  `json:"db"`
	Redis      componentHealth  `json:"redis"`
	Queue      queueHealth      `json:"queue"`
	Disk       []diskUsage      `json:"disk"`
	License    map[string]any   `json:"license"` // placeholder until Phase 5
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	resp := healthResponse{
		API:     componentHealth{Up: true}, // we are serving this request
		DB:      componentHealth{Up: pingDB(ctx)},
		Redis:   componentHealth{Up: pingRedis(ctx)},
		Queue:   queueHealthNow(ctx),
		Disk:    diskUsageAll(),
		License: map[string]any{"valid": true, "placeholder": true},
	}
	resp.FreeRADIUS = freeRADIUSHealth(ctx)
	httpapi.JSON(w, http.StatusOK, resp)
}

func pingDB(ctx context.Context) bool {
	if pkgDB == nil {
		return false
	}
	c, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	return pkgDB.Ping(c) == nil
}

func pingRedis(ctx context.Context) bool {
	if pkgRDB == nil {
		return false
	}
	c, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	return pkgRDB.Ping(c).Err() == nil
}

// queueHealthNow reads the acct counters (live endpoint first, pipeline_counters
// fallback) plus the drain-rate handoff and enforcement-failure counter.
func queueHealthNow(ctx context.Context) queueHealth {
	q := queueHealth{Counters: map[string]any{}}
	snap := fetchAcctCounters(ctx)
	if snap != nil {
		q.Counters = snap
		if v, ok := toInt64(snap["in_queue"]); ok {
			q.Depth = v
		}
		if v, ok := snap["invariant_ok"].(bool); ok {
			q.InvariantOK = v
		}
	}
	if pkgRDB != nil {
		c, cancel := context.WithTimeout(ctx, time.Second)
		if f, err := pkgRDB.Get(c, keyAcctDrain).Float64(); err == nil {
			q.DrainRate = f
		}
		if n, err := pkgRDB.Get(c, keyEnforceFailures).Int64(); err == nil {
			q.EnforcementFailures = n
		}
		cancel()
	}
	return q
}

// fetchAcctCounters reads the live pipeline counters via the shared helper,
// bound to the API process's pkg handles.
func fetchAcctCounters(ctx context.Context) map[string]any {
	return acctSnapshot(ctx, pkgDB, pkgRDB)
}

// freeRADIUSHealth derives request/reject rates from the decisions stream over
// the last minute and reads the monitor's Status-Server liveness handoff.
func freeRADIUSHealth(ctx context.Context) freeradiusHealth {
	h := freeradiusHealth{}
	if pkgRDB == nil {
		return h
	}
	c, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	if up, err := pkgRDB.Get(c, keyFreeRADIUSUp).Bool(); err == nil {
		h.Up = up
	}

	// Window the last minute of decisions by stream-ID timestamp.
	sinceMS := time.Now().Add(-time.Minute).UnixMilli()
	entries, err := pkgRDB.XRevRangeN(c, decisionStream, "+", "-", 2000).Result()
	if err != nil {
		return h
	}
	var total, rejects int
	for _, e := range entries {
		if ms := streamIDMillis(e.ID); ms > 0 && ms < sinceMS {
			break // older than the window; stream is time-ordered
		}
		total++
		if raw, ok := e.Values["event"].(string); ok {
			var ev struct {
				Outcome string `json:"outcome"`
			}
			if json.Unmarshal([]byte(raw), &ev) == nil && ev.Outcome == "reject" {
				rejects++
			}
		}
	}
	h.ReqRate = float64(total) / 60.0
	if total > 0 {
		h.RejectRate = float64(rejects) / float64(total)
		// Recent traffic is itself evidence FreeRADIUS is up if the probe key is
		// absent (monitor not running yet).
		if _, err := pkgRDB.Get(c, keyFreeRADIUSUp).Result(); err == redis.Nil {
			h.Up = true
		}
	}
	return h
}

func streamIDMillis(id string) int64 {
	if i := strings.IndexByte(id, '-'); i > 0 {
		if ms, err := strconv.ParseInt(id[:i], 10, 64); err == nil {
			return ms
		}
	}
	return 0
}

func toInt64(v any) (int64, bool) {
	switch n := v.(type) {
	case float64:
		return int64(n), true
	case int64:
		return n, true
	case json.Number:
		i, err := n.Int64()
		return i, err == nil
	}
	return 0, false
}
