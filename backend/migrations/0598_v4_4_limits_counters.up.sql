-- 0598_v4_4_limits_counters — v4-4 Limits engine (FR-128, FR-129).
--
-- subscriber_counters is the RESETTABLE per-limit state the phase brief calls
-- for: not a running total maintained incrementally (that invites double-
-- counting bugs on redelivery/restart, the exact class of bug the accounting
-- pipeline works hard to avoid elsewhere), but the two CYCLE ANCHORS uptime
-- and daily usage are computed FROM, live, the same way quota already is
-- (accounting/quota.go sums usage_points since subscribers.quota_cycle_anchor
-- on every read rather than maintaining a running counter). "Resetting" a
-- limit is exactly "moving its anchor forward" — the row's writes are the
-- resettable state; the amount used is always derived, never stored-and-
-- edited (the same invariant CLAUDE.md states for the money ledger).
--
-- v4-5 (FR-134) completes the counter-model split (lifetime vs. resettable
-- billing counters); this phase creates only what it needs: the uptime cycle
-- anchor (mirrors quota_cycle_anchor, reset on renewal — wired in v4-5/
-- billing, not here) and the daily period start (reset by radius/limits.go's
-- sweep at each profile's daily_reset_min, in the server timezone).
CREATE TABLE subscriber_counters (
    subscriber_id       uuid PRIMARY KEY REFERENCES subscribers(id) ON DELETE CASCADE,
    uptime_cycle_anchor timestamptz NOT NULL DEFAULT now(),
    daily_period_start  timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now()
);

CREATE TRIGGER subscriber_counters_set_updated_at
    BEFORE UPDATE ON subscriber_counters
    FOR EACH ROW EXECUTE FUNCTION business_touch_updated_at();

-- Widen enforcement_actions.event_kind (0220) to cover the new limit kinds
-- the worker (radius/limitsworker.go) claims/records: uptime and daily
-- crossings, speed-window entry/exit, and the daily-reset sweep itself (also
-- claimed, so a restart mid-reset cannot double-fire it — gate item 9).
DO $$
DECLARE r record;
BEGIN
    FOR r IN
        SELECT conname FROM pg_constraint
         WHERE conrelid = 'enforcement_actions'::regclass
           AND contype = 'c'
           AND pg_get_constraintdef(oid) ILIKE '%event_kind%'
    LOOP
        EXECUTE format('ALTER TABLE enforcement_actions DROP CONSTRAINT %I', r.conname);
    END LOOP;
END $$;
ALTER TABLE enforcement_actions ADD CONSTRAINT enforcement_actions_event_kind_check
    CHECK (event_kind IN (
        'quota_exceeded', 'expired', 'tod',
        'uptime_exceeded', 'daily_exceeded', 'window_change', 'daily_reset'
    ));
