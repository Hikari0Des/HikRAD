package main

// FR-40 counter reads. CounterSnapshot mirrors internal/accounting's JSON
// wire shape (GET /internal/acct/counters) as an independent client-side
// type on purpose: this tool talks to hikrad-acct only over HTTP, the same
// boundary FreeRADIUS/the pilot operator would use.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type CounterSnapshot struct {
	Received     int64 `json:"received"`
	Enqueued     int64 `json:"enqueued"`
	Spilled      int64 `json:"spilled"`
	Drained      int64 `json:"drained"`
	Persisted    int64 `json:"persisted"`
	Deduplicated int64 `json:"deduplicated"`
	Reaped       int64 `json:"reaped"`
	OrphanStops  int64 `json:"orphan_stops"`
	InQueue      int64 `json:"in_queue"`
	InvariantOK  bool  `json:"invariant_ok"`
}

func fetchCounters(addr string) (CounterSnapshot, error) {
	var s CounterSnapshot
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+addr+"/internal/acct/counters", nil)
	if err != nil {
		return s, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return s, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return s, fmt.Errorf("counters status %d", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
		return s, err
	}
	return s, nil
}

// waitInvariant polls the counters endpoint until in_queue drains to 0 and
// the invariant holds, or timeout elapses. Draining a large backlog after a
// recovery is not instant, so scenarios give this real headroom.
func waitInvariant(addr string, timeout time.Duration) (CounterSnapshot, bool) {
	deadline := time.Now().Add(timeout)
	var last CounterSnapshot
	for time.Now().Before(deadline) {
		s, err := fetchCounters(addr)
		if err == nil {
			last = s
			if s.InQueue == 0 && s.InvariantOK {
				return s, true
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	return last, false
}

// sessionCounts reconciles what actually landed in Postgres for a NAS: the
// session-state-consistency half of the DoD (not just the counter math).
func (r *Rig) sessionCounts(ctx context.Context, nasID string) (sessions, usagePoints int64, err error) {
	if err = r.db.QueryRow(ctx, `SELECT count(*) FROM sessions WHERE nas_id=$1`, nasID).Scan(&sessions); err != nil {
		return
	}
	err = r.db.QueryRow(ctx, `SELECT count(*) FROM usage_points WHERE nas_id=$1`, nasID).Scan(&usagePoints)
	return
}
