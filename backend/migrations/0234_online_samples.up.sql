-- 0234_online_samples — Phase 3, Agent 3. Contract C5 / FR-32.
-- Per-minute online-session counts recorded by the monitor's sampler, the source
-- of the dashboard's 24 h sparkline (downsampled at read time). A tiny hypertable
-- rather than a Redis list so the series survives a restart and TimescaleDB does
-- the time-bucketing for the sparkline.
CREATE TABLE online_samples (
    at     timestamptz NOT NULL DEFAULT now(),
    online integer NOT NULL DEFAULT 0
);

SELECT create_hypertable('online_samples', 'at', chunk_time_interval => INTERVAL '7 days');

-- Only ~10 days of one-minute samples are ever read (24 h sparkline + slack);
-- keep the table bounded.
SELECT add_retention_policy('online_samples', INTERVAL '10 days');
