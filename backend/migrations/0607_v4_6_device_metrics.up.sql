-- 0607 — v4-6 (Router manager I), FR-139/FR-141, migration budget 0606-0608
-- amended to 0607-0609: the repo's real max at kickoff was 0606 (v4-5's last
-- migration), so this phase starts at the next free number per the master
-- plan's linear-sequence rule (§6.2), not its stated budget.
--
-- device_metrics: one sample per enabled NAS per device-worker tick (radius
-- package's startDeviceWorker, same process as hikrad-api — see device_worker.go),
-- sourced from the RouterOS API's /system/resource and active-session counts.
-- This is what the monitor grid (FR-139) and the Overview tab's history charts
-- (FR-141) read — NEVER a synchronous router call in the request path; the
-- worker is the only writer, HTTP handlers only ever SELECT the latest/range.
CREATE TABLE device_metrics (
    time          timestamptz NOT NULL,
    nas_id        uuid NOT NULL,
    reachable     boolean NOT NULL,
    ros_version   text NOT NULL DEFAULT '',
    board_name    text NOT NULL DEFAULT '',
    identity      text NOT NULL DEFAULT '',
    serial_number text NOT NULL DEFAULT '',
    cpu_percent   double precision,
    cpu_cores     int,
    mem_percent   double precision,
    session_count int,
    uptime_secs   bigint
);

SELECT create_hypertable('device_metrics', 'time', chunk_time_interval => INTERVAL '1 day');

-- Per-NAS range scans (monitor grid's "latest row" and Overview's history
-- chart) are the only read pattern.
CREATE INDEX device_metrics_nas_idx ON device_metrics (nas_id, time DESC);

-- Sampled at most once a minute per NAS (worker cadence, see device_worker.go);
-- 90 days of history is ample for the Overview charts and cheap at that rate.
ALTER TABLE device_metrics SET (
    timescaledb.compress,
    timescaledb.compress_segmentby = 'nas_id',
    timescaledb.compress_orderby   = 'time DESC'
);
SELECT add_compression_policy('device_metrics', INTERVAL '7 days');
SELECT add_retention_policy('device_metrics', INTERVAL '90 days');
