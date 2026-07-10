-- 0131_usage_points — Phase 2, Agent 3. Contract C1-C / FR-33 / FR-37.2.
-- The raw traffic-delta hypertable: one row per accounting interval per session,
-- carrying the download/upload bytes accrued since the previous record. Keyed by
-- NAS event_time (not receipt time) so out-of-order interims land in the right
-- bucket (FR-37.4). Quota math sums delta_down/up over service='pppoe' rows
-- only (FR-58.3); usage_daily (0132) rolls up all services for graphs.
CREATE TABLE usage_points (
    time          timestamptz NOT NULL,
    subscriber_id uuid,
    nas_id        uuid,
    delta_down    bigint NOT NULL DEFAULT 0,
    delta_up      bigint NOT NULL DEFAULT 0,
    -- Quota-exempt traffic (walled-garden / free destinations); excluded from
    -- quota accrual but still graphed. Reserved for FR-58/quota policy use.
    exempt        boolean NOT NULL DEFAULT false,
    -- Derived from nas.type per amended C1-C (FR-58).
    service       text NOT NULL DEFAULT 'pppoe'
);

-- Hypertable partitioned on time; 7-day chunks keep the compression/retention
-- boundaries coarse enough for the NFR-3 tier.
SELECT create_hypertable('usage_points', 'time', chunk_time_interval => INTERVAL '7 days');

-- Per-subscriber and per-NAS graph reads scan a time range for one id.
CREATE INDEX usage_points_subscriber_idx ON usage_points (subscriber_id, time DESC);
CREATE INDEX usage_points_nas_idx        ON usage_points (nas_id, time DESC);

-- Compress chunks older than 30 days (FR-33): segment by subscriber so
-- per-user range scans stay selective on compressed data.
ALTER TABLE usage_points SET (
    timescaledb.compress,
    timescaledb.compress_segmentby = 'subscriber_id',
    timescaledb.compress_orderby   = 'time DESC'
);
SELECT add_compression_policy('usage_points', INTERVAL '30 days');

-- Retention floor (FR-33 / FR-37.4): raw usage kept ≥ 12 months. 400 days keeps
-- us safely above the floor; a settings-driven adjuster can widen it later but
-- must never drop below the 12-month floor.
SELECT add_retention_policy('usage_points', INTERVAL '400 days');
