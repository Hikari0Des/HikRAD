package accounting

// Lifetime billing counters (v4-5, FR-134, migration 0602). subscriber_counters
// (v4-4) is the RESETTABLE per-limit state a renewal/reset action moves an
// anchor forward on; subscriber_billing_counters is the opposite number —
// lifetime totals a billing dispute needs, which must survive every reset
// untouched. This file is the ONLY writer (the phase brief's frozen contract:
// "never written by a reset; only ever incremented by the accounting
// consumer") — no reset/renewal code path anywhere else may touch this table,
// which internal/subscribers/billingcounters_guard_test.go enforces by
// grepping the whole module for write statements against it.
//
// Unlike subscriber_counters, which derives its "used" figure live from a
// bounded recent window on every read, lifetime totals sum an ever-growing,
// unbounded history — deriving them live would get slower every day forever.
// So this is an incrementally-maintained running total instead, updated
// inside the SAME transaction as the session/usage-point write it derives
// from, so a crash between them is impossible (the whole record either
// commits together or neither half does — the same all-or-nothing discipline
// upsertSession's own writes already follow).

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

// sqlExecer is the common surface of pgx.Tx and *pgxpool.Pool this file needs
// — the byte counters always run inside the consumer's per-record transaction
// (a pgx.Tx), while the reaper's lifetime-seconds credit runs standalone
// against the pool (its synthesized Stop is a single fire-and-forget
// statement, not part of a larger transaction).
type sqlExecer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// incrementLifetimeBytes adds a byte delta to the subscriber's lifetime
// totals, creating the row on first touch. Called wherever a usage_points row
// is inserted, with the identical deltas, so the two are always in lockstep —
// a subscriber's lifetime bytes are exactly the sum of every usage_points row
// ever written for them, by construction, not by a periodic reconciliation.
func incrementLifetimeBytes(ctx context.Context, tx sqlExecer, subscriberID string, deltaDown, deltaUp uint64, seenAt time.Time) error {
	if subscriberID == "" {
		return nil
	}
	_, err := tx.Exec(ctx,
		`INSERT INTO subscriber_billing_counters
		   (subscriber_id, lifetime_bytes_down, lifetime_bytes_up, first_seen_at, last_seen_at)
		 VALUES ($1::uuid, $2, $3, $4, $4)
		 ON CONFLICT (subscriber_id) DO UPDATE SET
		   lifetime_bytes_down = subscriber_billing_counters.lifetime_bytes_down + EXCLUDED.lifetime_bytes_down,
		   lifetime_bytes_up   = subscriber_billing_counters.lifetime_bytes_up   + EXCLUDED.lifetime_bytes_up,
		   first_seen_at       = LEAST(subscriber_billing_counters.first_seen_at, EXCLUDED.first_seen_at),
		   last_seen_at        = GREATEST(subscriber_billing_counters.last_seen_at, EXCLUDED.last_seen_at)`,
		subscriberID, int64(deltaDown), int64(deltaUp), seenAt)
	return err
}

// incrementLifetimeSeconds adds connected-time to the subscriber's lifetime
// total, counted once per session at Stop (including a reaper-synthesized
// stop) rather than per interim tick — a session still open when the process
// reads a subscriber's lifetime total simply has not contributed its final
// seconds yet, a documented small lag acceptable for a billing-dispute figure
// (contrast subscriber_counters' uptime limit, which DOES need a live count
// for enforcement and derives it from open sessions on every check).
func incrementLifetimeSeconds(ctx context.Context, tx sqlExecer, subscriberID string, seconds int64, seenAt time.Time) error {
	if subscriberID == "" || seconds <= 0 {
		return nil
	}
	_, err := tx.Exec(ctx,
		`INSERT INTO subscriber_billing_counters
		   (subscriber_id, lifetime_session_seconds, first_seen_at, last_seen_at)
		 VALUES ($1::uuid, $2, $3, $3)
		 ON CONFLICT (subscriber_id) DO UPDATE SET
		   lifetime_session_seconds = subscriber_billing_counters.lifetime_session_seconds + EXCLUDED.lifetime_session_seconds,
		   first_seen_at            = LEAST(subscriber_billing_counters.first_seen_at, EXCLUDED.first_seen_at),
		   last_seen_at             = GREATEST(subscriber_billing_counters.last_seen_at, EXCLUDED.last_seen_at)`,
		subscriberID, seconds, seenAt)
	return err
}
