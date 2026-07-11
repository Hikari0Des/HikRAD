package monitorsvc

// Shared accounting-counters reader used by both the health/dashboard APIs (in
// hikrad-api, via the pkg handles) and the monitor's condition loop (in
// hikrad-monitor, with its own handles). It prefers hikrad-acct's live counters
// endpoint and falls back to the durable pipeline_counters mirror so a momentary
// acct outage never blanks the invariant.

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

func acctURL() string {
	url := os.Getenv("HIKRAD_ACCT_URL")
	if url == "" {
		url = "http://hikrad-acct:8082"
	}
	return strings.TrimRight(url, "/")
}

// acctSnapshot returns the live pipeline counters (in_queue, invariant_ok, …).
func acctSnapshot(ctx context.Context, db *pgxpool.Pool, rdb *redis.Client) map[string]any {
	c, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(c, http.MethodGet, acctURL()+"/internal/acct/counters", nil)
	if err == nil {
		if resp, derr := (&http.Client{Timeout: 2 * time.Second}).Do(req); derr == nil {
			defer resp.Body.Close()
			if resp.StatusCode == 200 {
				var m map[string]any
				if json.NewDecoder(resp.Body).Decode(&m) == nil {
					return m
				}
			}
		}
	}
	return acctSnapshotFromDB(ctx, db, rdb)
}

func acctSnapshotFromDB(ctx context.Context, db *pgxpool.Pool, rdb *redis.Client) map[string]any {
	if db == nil {
		return nil
	}
	var received, persisted, deduped int64
	err := db.QueryRow(ctx,
		`SELECT received, persisted, deduplicated FROM pipeline_counters WHERE id`).
		Scan(&received, &persisted, &deduped)
	if err != nil {
		return nil
	}
	var depth int64
	if rdb != nil {
		c, cancel := context.WithTimeout(ctx, time.Second)
		if l, lerr := rdb.XLen(c, "acct:stream").Result(); lerr == nil {
			depth = l
		}
		cancel()
	}
	return map[string]any{
		"received":     received,
		"persisted":    persisted,
		"deduplicated": deduped,
		"in_queue":     depth,
		"invariant_ok": received-deduped-depth == persisted,
	}
}
