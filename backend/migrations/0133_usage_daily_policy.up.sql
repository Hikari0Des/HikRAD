-- 0133_usage_daily_policy — Phase 2, Agent 3. Contract C1-C / FR-33.
-- Refresh + retention policies for the usage_daily continuous aggregate (0132).
-- These are plain function calls, safe inside golang-migrate's implicit
-- transaction (unlike the aggregate creation itself).

-- Keep the last day materialized on a 1-hour cadence, trailing the live edge by
-- an hour so late/out-of-order interims (FR-37.4) are folded in before a bucket
-- is finalized.
SELECT add_continuous_aggregate_policy('usage_daily',
    start_offset      => INTERVAL '3 days',
    end_offset        => INTERVAL '1 hour',
    schedule_interval => INTERVAL '1 hour');

-- Rollup retention floor (FR-33): daily aggregates kept ≥ 3 years. 1100 days
-- (~3y) sits above the floor; never drop below it.
SELECT add_retention_policy('usage_daily', INTERVAL '1100 days');
