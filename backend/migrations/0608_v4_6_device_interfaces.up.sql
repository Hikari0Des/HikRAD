-- 0608 — v4-6 (Router manager I), FR-141. See 0607 for the migration-number note.
--
-- device_interfaces: the current state snapshot of every interface the device
-- worker last read (Interfaces tab, FR-141) — upserted in place each tick, not
-- appended, since the panel only ever wants "what does the router look like
-- now" plus the separate traffic history below. Never written to by an HTTP
-- handler; the worker is the sole writer.
-- rx_bytes/tx_bytes/sampled_at are the worker's own bookkeeping (the router's
-- raw cumulative counters from the previous tick) so it can derive a bps rate
-- from two consecutive reads without a second table — never returned to the
-- panel as-is, only the derived interface_traffic rows are.
CREATE TABLE device_interfaces (
    nas_id      uuid NOT NULL,
    name        text NOT NULL,
    type        text NOT NULL DEFAULT '',
    running     boolean NOT NULL DEFAULT false,
    disabled    boolean NOT NULL DEFAULT false,
    mac_address text NOT NULL DEFAULT '',
    mtu         int,
    comment     text NOT NULL DEFAULT '',
    rx_bytes    bigint NOT NULL DEFAULT 0,
    tx_bytes    bigint NOT NULL DEFAULT 0,
    -- rx_bps/tx_bps are the last DERIVED rate (same value this tick wrote to
    -- interface_traffic below), kept here too so the Interfaces tab's current
    -- state read needs no second table join; interface_traffic stays the sole
    -- source for the rate-over-time chart.
    rx_bps      bigint NOT NULL DEFAULT 0,
    tx_bps      bigint NOT NULL DEFAULT 0,
    sampled_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (nas_id, name)
);

-- interface_traffic: per-interface throughput history (rx/tx bits-per-second,
-- derived by the worker from consecutive byte-counter reads), the Interfaces
-- tab's per-interface history chart.
CREATE TABLE interface_traffic (
    time      timestamptz NOT NULL,
    nas_id    uuid NOT NULL,
    interface text NOT NULL,
    rx_bps    bigint NOT NULL DEFAULT 0,
    tx_bps    bigint NOT NULL DEFAULT 0
);

SELECT create_hypertable('interface_traffic', 'time', chunk_time_interval => INTERVAL '1 day');

CREATE INDEX interface_traffic_nas_if_idx ON interface_traffic (nas_id, interface, time DESC);

ALTER TABLE interface_traffic SET (
    timescaledb.compress,
    timescaledb.compress_segmentby = 'nas_id, interface',
    timescaledb.compress_orderby   = 'time DESC'
);
SELECT add_compression_policy('interface_traffic', INTERVAL '7 days');
SELECT add_retention_policy('interface_traffic', INTERVAL '30 days');
