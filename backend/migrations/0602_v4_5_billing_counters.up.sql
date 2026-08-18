-- 0602_v4_5_billing_counters — v4-5 Subscribers, counters & card groups
-- (FR-134, "do this first — everything else depends on it").
--
-- Today ONE set of counters (subscribers.quota_cycle_anchor +
-- subscriber_counters, v4-4) serves both jobs: "how much has this subscriber
-- consumed THIS PERIOD" (resettable, drives limit enforcement) and "how much
-- have they EVER consumed" (lifetime, drives billing disputes). Resetting a
-- quota on renewal today has no way to avoid also erasing history, because
-- there is no separate place lifetime totals live. This migration is the
-- data-model half of the fix: a new table that is NEVER touched by a reset,
-- and is the ONLY place lifetime totals are written from this point forward.
--
-- Unlike subscriber_counters (v4-4, 0598), which derives its "used" amount
-- LIVE from a bounded recent window (since a cycle anchor) on every read,
-- lifetime totals sum an EVER-GROWING, unbounded history — deriving them live
-- would get slower every day forever. So this table is the one exception to
-- the "derive, never store-and-edit" posture elsewhere in the schema: it is
-- an incrementally-maintained running total, written by exactly one writer
-- (internal/accounting's consumer, on every accounting record it durably
-- persists) and read-only everywhere else. "Resetting" a limit is still
-- "move an anchor forward" on subscriber_counters, which stays completely
-- untouched by this migration and by everything this migration adds.
CREATE TABLE subscriber_billing_counters (
    subscriber_id            uuid PRIMARY KEY REFERENCES subscribers(id) ON DELETE CASCADE,
    lifetime_bytes_down      bigint NOT NULL DEFAULT 0,
    lifetime_bytes_up        bigint NOT NULL DEFAULT 0,
    lifetime_session_seconds bigint NOT NULL DEFAULT 0,
    first_seen_at            timestamptz,
    last_seen_at             timestamptz,
    updated_at               timestamptz NOT NULL DEFAULT now()
);

CREATE TRIGGER subscriber_billing_counters_set_updated_at
    BEFORE UPDATE ON subscriber_billing_counters
    FOR EACH ROW EXECUTE FUNCTION business_touch_updated_at();

-- Backfill (gate item 2: lossless — sum of lifetime bytes before migration ==
-- after, on a restored realistic dataset). Sourced from the same tables the
-- live derivations already trust (usage_points for bytes, sessions for
-- connected time), summed across EACH subscriber's ENTIRE history — not since
-- any cycle anchor, because this is the lifetime figure the anchors must never
-- affect. One row per subscriber that has ANY accounting history; a
-- subscriber with none gets no row (created lazily on first real usage,
-- mirroring subscriber_counters' own lazy-creation posture) rather than a
-- meaningless all-zero row.
INSERT INTO subscriber_billing_counters
    (subscriber_id, lifetime_bytes_down, lifetime_bytes_up, lifetime_session_seconds,
     first_seen_at, last_seen_at)
SELECT
    sub.id,
    COALESCE(u.bytes_down, 0),
    COALESCE(u.bytes_up, 0),
    COALESCE(s.session_seconds, 0),
    LEAST(u.first_seen, s.first_seen),
    GREATEST(u.last_seen, s.last_seen)
FROM subscribers sub
LEFT JOIN (
    SELECT subscriber_id,
           SUM(delta_down)::bigint AS bytes_down,
           SUM(delta_up)::bigint   AS bytes_up,
           MIN(time)               AS first_seen,
           MAX(time)               AS last_seen
      FROM usage_points
     WHERE subscriber_id IS NOT NULL
     GROUP BY subscriber_id
) u ON u.subscriber_id = sub.id
LEFT JOIN (
    SELECT subscriber_id,
           SUM(EXTRACT(EPOCH FROM (COALESCE(stopped_at, last_interim_at, started_at) - started_at)))::bigint AS session_seconds,
           MIN(started_at) AS first_seen,
           MAX(COALESCE(stopped_at, last_interim_at, started_at)) AS last_seen
      FROM sessions
     WHERE subscriber_id IS NOT NULL AND started_at IS NOT NULL
     GROUP BY subscriber_id
) s ON s.subscriber_id = sub.id
WHERE u.subscriber_id IS NOT NULL OR s.subscriber_id IS NOT NULL;
