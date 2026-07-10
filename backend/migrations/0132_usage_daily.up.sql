-- 0132_usage_daily — Phase 2, Agent 3. Contract C1-C / FR-33.
-- Daily download/upload rollup per subscriber, per NAS, per service. The
-- network-wide aggregate is derived by summing across ids at query time. Graphs
-- and reports read this (all services); quota math reads raw usage_points.
--
-- IMPORTANT: a TimescaleDB continuous aggregate cannot be created inside a
-- transaction block, and golang-migrate runs each file as one implicit
-- transaction. This file therefore contains EXACTLY ONE statement so it runs in
-- autocommit; the refresh/retention policies live in 0133.
-- materialized_only=false keeps real-time aggregation ON (the default flipped
-- across TimescaleDB versions): a query against usage_daily transparently unions
-- the materialized buckets with the not-yet-materialized recent raw points, so
-- graphs are never stale by the refresh interval.
CREATE MATERIALIZED VIEW usage_daily
    WITH (timescaledb.continuous, timescaledb.materialized_only = false) AS
    SELECT time_bucket(INTERVAL '1 day', time) AS day,
           subscriber_id,
           nas_id,
           service,
           sum(delta_down) AS down_bytes,
           sum(delta_up)   AS up_bytes
      FROM usage_points
     GROUP BY day, subscriber_id, nas_id, service
    WITH NO DATA;
