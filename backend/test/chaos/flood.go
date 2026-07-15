package main

// Accounting-flood generator. Speaks the exact C6 ingest wire shape
// (internal/accounting.Record's JSON tags) directly to hikrad-acct's
// POST /acct — the same HTTP boundary FreeRADIUS's rlm_rest forward hits.
// FreeRADIUS/rlm_rest wiring itself is B's territory (deploy/freeradius,
// unstaffed this phase); testing at this boundary is the correct,
// reproducible substitute that still exercises 100% of the lossless
// pipeline this suite is chartered to prove (FR-37/38/40).
//
// On any POST failure this generator retries with backoff until
// -retry-budget elapses, standing in for the NAS retransmit-on-non-2xx
// behavior the real contract depends on (ingest.go: "fails closed... the
// NAS retransmits").

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

type recordStep struct {
	typ               string
	secs              int
	bytesIn, bytesOut uint64
}

func planSteps(interims int) []recordStep {
	steps := []recordStep{{typ: "start", secs: 0}}
	for i := 1; i <= interims; i++ {
		secs := i * 30
		steps = append(steps, recordStep{typ: "interim", secs: secs, bytesIn: uint64(secs) * 1000, bytesOut: uint64(secs) * 2000})
	}
	last := steps[len(steps)-1]
	steps = append(steps, recordStep{typ: "stop", secs: last.secs + 30, bytesIn: last.bytesIn + 1000, bytesOut: last.bytesOut + 2000})
	return steps
}

type simSession struct {
	nasIP, acctID, user string
	base                time.Time
	steps               []recordStep
	next                int
}

func newSessions(n int, nasIP string, interims int) []*simSession {
	steps := planSteps(interims)
	base := time.Now().Add(-time.Hour).UTC()
	out := make([]*simSession, n)
	for i := range out {
		id := fmt.Sprintf("%s-%d", randHex(4), i)
		out[i] = &simSession{
			nasIP:  nasIP,
			acctID: "chaos-" + id,
			user:   "chaos-user-" + id,
			base:   base,
			steps:  steps,
		}
	}
	return out
}

func buildRecord(s *simSession, step recordStep) map[string]any {
	return map[string]any{
		"record_type":        step.typ,
		"nas_ip":              s.nasIP,
		"acct_session_id":     s.acctID,
		"username":            s.user,
		"framed_ip":           "100.64.0.1",
		"calling_station_id":  "AA:BB:CC:DD:EE:FF",
		"session_time":        step.secs,
		"bytes_in":            step.bytesIn,
		"bytes_out":           step.bytesOut,
		"gigawords_in":        0,
		"gigawords_out":       0,
		"event_time":          fmt.Sprintf("%d", s.base.Add(time.Duration(step.secs)*time.Second).Unix()),
	}
}

// postRecord POSTs one record, retrying on failure until retryBudget
// elapses. acked=false means the record was never durably accepted within
// budget — a genuine loss risk the scenario must fail on.
func postRecord(addr string, rec map[string]any, retryBudget time.Duration) (acked bool, attempts int64, lastLatency time.Duration) {
	body, _ := json.Marshal(rec)
	deadline := time.Now().Add(retryBudget)
	for {
		attempts++
		start := time.Now()
		ok := postOnce(addr, body)
		lastLatency = time.Since(start)
		if ok {
			return true, attempts, lastLatency
		}
		if time.Now().After(deadline) {
			return false, attempts, lastLatency
		}
		time.Sleep(300 * time.Millisecond)
	}
}

func postOnce(addr string, body []byte) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+addr+"/acct", bytes.NewReader(body))
	if err != nil {
		return false
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusNoContent
}

// FloodOpts drives a steady-rate flood across a session pool.
type FloodOpts struct {
	AcctAddr       string
	Sessions       []*simSession
	Rate           float64 // aggregate packets/sec
	Duration       time.Duration
	RetransmitEach int // >1 models a NAS retransmit storm: same record sent N times back-to-back
	RetryBudget    time.Duration
}

type FloodResult struct {
	Attempts      int64
	UniqueRecords int64
	Acked         int64
	FailedFinal   int64
	Elapsed       time.Duration
}

func runFlood(opts FloodOpts) FloodResult {
	if opts.RetryBudget <= 0 {
		opts.RetryBudget = 60 * time.Second
	}
	retransmit := opts.RetransmitEach
	if retransmit < 1 {
		retransmit = 1
	}
	interval := time.Duration(float64(time.Second) / opts.Rate)
	if interval <= 0 {
		interval = time.Millisecond
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	var attempts, unique, acked, failedFinal int64
	var wg sync.WaitGroup
	start := time.Now()
	deadline := start.Add(opts.Duration)
	idx := 0
	n := len(opts.Sessions)

	for time.Now().Before(deadline) {
		<-ticker.C
		// Round-robin to the next session with a pending step.
		var sess *simSession
		for tries := 0; tries < n; tries++ {
			cand := opts.Sessions[idx%n]
			idx++
			if cand.next < len(cand.steps) {
				sess = cand
				break
			}
		}
		if sess == nil {
			break // every session finished its plan
		}
		step := sess.steps[sess.next]
		sess.next++
		rec := buildRecord(sess, step)
		atomic.AddInt64(&unique, 1)

		wg.Add(1)
		go func() {
			defer wg.Done()
			anyAcked := false
			for i := 0; i < retransmit; i++ {
				ok, n, _ := postRecord(opts.AcctAddr, rec, opts.RetryBudget)
				atomic.AddInt64(&attempts, n)
				if ok {
					anyAcked = true
				}
			}
			if anyAcked {
				atomic.AddInt64(&acked, 1)
			} else {
				atomic.AddInt64(&failedFinal, 1)
			}
		}()
	}
	wg.Wait()

	return FloodResult{
		Attempts:      atomic.LoadInt64(&attempts),
		UniqueRecords: atomic.LoadInt64(&unique),
		Acked:         atomic.LoadInt64(&acked),
		FailedFinal:   atomic.LoadInt64(&failedFinal),
		Elapsed:       time.Since(start),
	}
}
