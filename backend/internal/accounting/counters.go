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

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
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
// stay monotonic across restarts. rdb may be nil (unit tests) — received/
// enqueued then come from Postgres alone, same as before this existed.
func (c *counters) load(ctx context.Context, db *pgxpool.Pool, rdb *redis.Client) error {
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
	// received/enqueued are also mirrored into Redis on every event (see
	// bumpRedisCounter's callers), which survives an unclean crash the
	// Postgres periodic flush might have missed — prefer whichever source
	// has seen more, since both are monotonic and neither can legitimately
	// be ahead of reality.
	if rReceived := redisCounterValue(ctx, rdb, counterReceivedKey); rReceived > s.Received {
		s.Received = rReceived
	}
	if rEnqueued := redisCounterValue(ctx, rdb, counterEnqueuedKey); rEnqueued > s.Enqueued {
		s.Enqueued = rEnqueued
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

// loadCountersWithRetry bounds-retries counters.load at boot. A single-shot
// load (the original behavior) permanently zeroes every counter — not just
// received/enqueued's Redis-mirrored ones — whenever hikrad-acct restarts
// while Postgres is still mid-recovery from an unclean shutdown, which
// `depends_on: service_healthy` prevents on a fresh `docker compose up` but
// does NOT prevent for a `restart: unless-stopped` recovery after a real
// host power-loss (Docker's per-container restart policy does not re-check
// dependency health) — exactly what "unclean reboot" means. Found by the
// Phase-5 chaos suite's unclean-reboot scenario. Bounded to keep the
// documented "ingest must not hard-fail waiting on the DB" boot posture
// intact for the case Postgres is genuinely down for longer than this.
func loadCountersWithRetry(ctx context.Context, c *counters, db *pgxpool.Pool, rdb *redis.Client) error {
	deadline := time.Now().Add(20 * time.Second)
	var lastErr error
	for {
		if lastErr = c.load(ctx, db, rdb); lastErr == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return lastErr
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(1 * time.Second):
		}
	}
}

// flush writes the current received/enqueued/spilled/drained totals to the
// durable mirror. persisted/deduplicated/reaped/orphan_stops are
// deliberately NOT included: they are now bumped durably per-event
// (bumpPersistedInTx, bumpCounterDurable), each with exactly one writer. An
// absolute periodic SET for those columns from this process's in-memory
// snapshot — taken at some earlier instant — could otherwise race a
// per-event relative "col = col + 1" and clobber it back down (a lost
// update): the snapshot is captured before the per-event bump commits, but
// this flush's own UPDATE executes after it, overwriting the correct value
// with the stale one. Found by the Phase-5 chaos suite's unclean-reboot
// scenario (persisted/deduplicated came out durably wrong even though both
// write paths were individually correct).
func (c *counters) flush(ctx context.Context, db *pgxpool.Pool) error {
	if db == nil {
		return nil
	}
	s := c.snapshot(0)
	_, err := db.Exec(ctx,
		`UPDATE pipeline_counters
		    SET received=$1, enqueued=$2, spilled=$3, drained=$4, updated_at=now()
		  WHERE id`,
		s.Received, s.Enqueued, s.Spilled, s.Drained)
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

// bumpPersistedInTx durably increments persisted (and orphan_stops, for an
// orphan Stop) as part of the caller's own transaction, so the durable
// mirror commits atomically with the session/usage row it counts — the
// periodic flush (runCounterFlusher, every 5s) alone leaves a real gap: a
// process that crashes before its first tick (or is SIGKILLed, skipping the
// on-shutdown flush) resets pipeline_counters to whatever the LAST flush
// wrote — 0 if none ever fired — even though every record up to the crash
// was correctly persisted. A restarted instance then loads that stale 0 and
// under-reports every count from then on: the invariant still holds
// (self-consistent from the new instance's own view) but the totals no
// longer reflect reality, undermining the one thing FR-40 exists to prove.
// Found by the Phase-5 chaos suite's kill-acct scenario (real session/usage
// rows: 100% correct; pipeline_counters: reset to zero).
func bumpPersistedInTx(ctx context.Context, tx pgx.Tx, orphan bool) error {
	if orphan {
		_, err := tx.Exec(ctx, `UPDATE pipeline_counters SET persisted = persisted + 1, orphan_stops = orphan_stops + 1, updated_at = now() WHERE id`)
		return err
	}
	_, err := tx.Exec(ctx, `UPDATE pipeline_counters SET persisted = persisted + 1, updated_at = now() WHERE id`)
	return err
}

// bumpCounterDurable increments one pipeline_counters column immediately
// (its own committed statement, not the caller's transaction — used from the
// dedup path, which rolls its transaction back, and from the reaper, which
// doesn't hold one open). Best-effort: a failure here only widens the same
// pre-existing periodic-flush gap for this one column, it never risks the
// data path.
func bumpCounterDurable(ctx context.Context, db *pgxpool.Pool, column string) {
	if db == nil {
		return
	}
	var sql string
	switch column {
	case "deduplicated":
		sql = `UPDATE pipeline_counters SET deduplicated = deduplicated + 1, updated_at = now() WHERE id`
	case "reaped":
		sql = `UPDATE pipeline_counters SET reaped = reaped + 1, updated_at = now() WHERE id`
	default:
		return
	}
	cctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	_, _ = db.Exec(cctx, sql)
}
