package perfutil

import (
	"bytes"
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

func FetchCounters(addr string) (CounterSnapshot, error) {
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
	return s, json.NewDecoder(resp.Body).Decode(&s)
}

// PostRecord POSTs one C6-shaped accounting record to addr's /acct and
// returns whether it was durably acked (204) and the round-trip latency.
func PostRecord(addr string, rec map[string]any) (ok bool, latency time.Duration) {
	body, _ := json.Marshal(rec)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+addr+"/acct", bytes.NewReader(body))
	if err != nil {
		return false, 0
	}
	req.Header.Set("Content-Type", "application/json")
	start := time.Now()
	resp, err := http.DefaultClient.Do(req)
	latency = time.Since(start)
	if err != nil {
		return false, latency
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusNoContent, latency
}

// BuildRecord mirrors internal/accounting.Record's JSON wire shape (C6).
func BuildRecord(nasIP, acctSessionID, username, recordType string, base time.Time, secs int, bytesIn, bytesOut uint64) map[string]any {
	return map[string]any{
		"record_type":       recordType,
		"nas_ip":             nasIP,
		"acct_session_id":    acctSessionID,
		"username":           username,
		"framed_ip":          "100.64.0.1",
		"calling_station_id": "AA:BB:CC:DD:EE:FF",
		"session_time":       secs,
		"bytes_in":           bytesIn,
		"bytes_out":          bytesOut,
		"gigawords_in":       0,
		"gigawords_out":      0,
		"event_time":         fmt.Sprintf("%d", base.Add(time.Duration(secs)*time.Second).Unix()),
	}
}
