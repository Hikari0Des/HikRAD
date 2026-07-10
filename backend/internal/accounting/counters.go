package accounting

// Pipeline audit counters (FR-40, contract C1-C). Every stage of the pipeline
// runs inside the single hikrad-acct process, so the hot counters are
// in-process atomics — they keep counting even when Redis is down (the ingest
// spill path increments `spilled` with no Redis at all). They are loaded from
// pipeline_counters at boot and flushed back periodically and on shutdown, so
// the monotonic totals survive a restart (DoD). in_queue is measured live from
// the stream + spill so the invariant is a real conservation check, not an
// identity.

import (
	"context"
	"encoding/json"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// counters holds the monotonic pipeline totals.
type counters struct {
	received     atomic.Int64
	enqueued     atomic.Int64
	spilled      atomic.Int64
	drained      atomic.Int64
	persisted    atomic.Int64
	deduplicated atomic.Int64
	reaped       atomic.Int64
	orphanStops  atomic.Int64
}

// CounterSnapshot is the JSON shape returned by /internal/acct/counters and the
// row shape mirrored to pipeline_counters. InQueue and InvariantOK are computed
// at read time.
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

func (c *counters) snapshot(inQueue int64) CounterSnapshot {
	s := CounterSnapshot{
		Received:     c.received.Load(),
		Enqueued:     c.enqueued.Load(),
		Spilled:      c.spilled.Load(),
		Drained:      c.drained.Load(),
		Persisted:    c.persisted.Load(),
		Deduplicated: c.deduplicated.Load(),
		Reaped:       c.reaped.Load(),
		OrphanStops:  c.orphanStops.Load(),
		InQueue:      inQueue,
	}
	// FR-40 invariant: every received packet is eventually either persisted or
	// dropped as a duplicate; the rest are still in flight.
	s.InvariantOK = s.Received-s.Deduplicated-s.InQueue == s.Persisted
	return s
}

// loadCounters primes the in-process atomics from the durable mirror so totals
// stay monotonic across restarts.
func (c *counters) load(ctx context.Context, db *pgxpool.Pool) error {
	if db == nil {
		return nil
	}
	var s CounterSnapshot
	err := db.QueryRow(ctx,
		`SELECT received, enqueued, spilled, drained, persisted, deduplicated, reaped, orphan_stops
		   FROM pipeline_counters WHERE id`).
		Scan(&s.Received, &s.Enqueued, &s.Spilled, &s.Drained, &s.Persisted, &s.Deduplicated, &s.Reaped, &s.OrphanStops)
	if err != nil {
		return err
	}
	c.received.Store(s.Received)
	c.enqueued.Store(s.Enqueued)
	c.spilled.Store(s.Spilled)
	c.drained.Store(s.Drained)
	c.persisted.Store(s.Persisted)
	c.deduplicated.Store(s.Deduplicated)
	c.reaped.Store(s.Reaped)
	c.orphanStops.Store(s.OrphanStops)
	return nil
}

// flush writes the current totals to the durable mirror.
func (c *counters) flush(ctx context.Context, db *pgxpool.Pool) error {
	if db == nil {
		return nil
	}
	s := c.snapshot(0)
	_, err := db.Exec(ctx,
		`UPDATE pipeline_counters
		    SET received=$1, enqueued=$2, spilled=$3, drained=$4, persisted=$5,
		        deduplicated=$6, reaped=$7, orphan_stops=$8, updated_at=now()
		  WHERE id`,
		s.Received, s.Enqueued, s.Spilled, s.Drained, s.Persisted,
		s.Deduplicated, s.Reaped, s.OrphanStops)
	return err
}

// countersHandler serves GET /internal/acct/counters with the live invariant.
func (s *Service) countersHandler(w http.ResponseWriter, r *http.Request) {
	inQueue := s.inQueue(r.Context())
	snap := s.counters.snapshot(inQueue)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(snap)
}

// inQueue measures packets that are received but not yet terminal: stream
// entries not yet acked/deleted plus records still parked in the disk spill.
func (s *Service) inQueue(ctx context.Context) int64 {
	var n int64
	if s.rdb != nil {
		cctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		if l, err := s.rdb.XLen(cctx, streamKey).Result(); err == nil {
			n += l
		}
		cancel()
	}
	n += int64(s.spill.pending())
	return n
}
