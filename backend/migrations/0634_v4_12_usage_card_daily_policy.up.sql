-- 0634_v4_12_usage_card_daily_policy — refresh + retention policies for
-- usage_card_daily (0633), mirroring 0133's usage_daily policy exactly (same
-- cadence, same offsets, same retention floor) — no reason for the card
-- rollup to be governed differently from the subscriber rollup it sits
-- beside. Plain function calls, safe inside golang-migrate's implicit
-- transaction (unlike 0633's aggregate creation itself).
SELECT add_continuous_aggregate_policy('usage_card_daily',
    start_offset      => INTERVAL '3 days',
    end_offset        => INTERVAL '1 hour',
    schedule_interval => INTERVAL '1 hour');

SELECT add_retention_policy('usage_card_daily', INTERVAL '1100 days');
