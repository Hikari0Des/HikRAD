package accounting

// Ingest HTTP handler (contract C6, FR-37.1). FreeRADIUS forwards every
// Accounting-Request here; we reply 204 ONLY after the record is durably
// enqueued — on the Redis stream, or (Redis down) in the fsync'd disk spill.
// Any other outcome is a non-2xx so the FreeRADIUS exec script fails closed and
// the NAS retransmits. That is the whole lossless contract in one function.

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"
)

// maxBodyBytes caps an ingest body; a well-formed accounting forward is a few
// hundred bytes.
const maxBodyBytes = 64 * 1024

func (s *Service) ingestHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBodyBytes))
	if err != nil {
		http.Error(w, "read body", http.StatusBadRequest)
		return
	}
	rec, err := parseRecord(body, s.now())
	if err != nil {
		// Malformed / non-session packet: a 400 avoids a retransmit storm for
		// something we can never accept. Genuine session records never land here.
		s.log.Warn("acct ingest: reject", "error", err)
		http.Error(w, "bad accounting record", http.StatusBadRequest)
		return
	}

	// received counts the packet the instant it is accepted for durability,
	// before we know whether it lands on the stream or the spill.
	s.counters.received.Add(1)

	payload, err := json.Marshal(rec)
	if err != nil {
		http.Error(w, "encode", http.StatusInternalServerError)
		return
	}

	if err := s.durablyEnqueue(r.Context(), payload); err != nil {
		// Neither the stream nor the spill could persist it: do NOT ack. The NAS
		// retransmits; nothing is lost.
		s.log.Error("acct ingest: not durable, failing closed", "error", err,
			"acct_session_id", rec.AcctSessionID, "type", rec.RecordType)
		http.Error(w, "not durable", http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// durablyEnqueue appends to the Redis stream, falling back to the disk spill
// when Redis is unavailable. Exactly one of enqueued/spilled is incremented on
// success.
func (s *Service) durablyEnqueue(ctx context.Context, payload []byte) error {
	cctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	if s.rdb != nil {
		if _, err := enqueueStream(cctx, s.rdb, payload); err == nil {
			s.counters.enqueued.Add(1)
			return nil
		} else {
			s.log.Warn("acct ingest: stream append failed, spilling to disk", "error", err)
		}
	}

	if err := s.spill.append(payload); err != nil {
		return err
	}
	s.counters.spilled.Add(1)
	return nil
}
