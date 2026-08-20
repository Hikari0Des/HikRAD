-- 0633_v4_12_usage_card_daily — v4-12 (FR-173). Daily download/upload rollup
-- per card, per NAS, per service — the card-tab sibling of 0132's
-- usage_daily. Needed because 0604 (v4-5 cards) added card_id to
-- usage_points/sessions but never to the usage_daily continuous aggregate, so
-- there was no rollup a card-scoped query could read without scanning the raw
-- hypertable — exactly what FR-47 already forbids at panel-load budgets. This
-- migration closes that gap; it is not a "measure first, maybe skip" index,
-- it is a rollup that plainly does not exist yet.
--
-- Same autocommit constraint as 0132: a continuous aggregate cannot be
-- created inside a transaction block, and golang-migrate runs each file as
-- one implicit transaction, so this file contains EXACTLY ONE statement.
-- Refresh/retention policies live in 0634, mirroring the 0132/0133 split.
-- materialized_only=false for the same reason 0132 sets it: a query
-- transparently unions materialized buckets with just-written raw points, so
-- today's card traffic is never stale by the refresh interval.
CREATE MATERIALIZED VIEW usage_card_daily
    WITH (timescaledb.continuous, timescaledb.materialized_only = false) AS
    SELECT time_bucket(INTERVAL '1 day', time) AS day,
           card_id,
           nas_id,
           service,
           sum(delta_down) AS down_bytes,
           sum(delta_up)   AS up_bytes
      FROM usage_points
     WHERE card_id IS NOT NULL
     GROUP BY day, card_id, nas_id, service
    WITH NO DATA;
