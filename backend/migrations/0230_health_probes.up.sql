-- 0230_health_probes — Phase 3, Agent 3 (Accounting & Monitoring), range 0230–0239.
-- Contract C1-C / C5 / FR-34. The raw probe-result hypertable: one row per ICMP
-- (15 s) or SNMP (60 s) probe of a target. A target is EITHER a NAS (nas_id set)
-- or a monitored device (device_id set, FR-60) — never both, never neither. The
-- probe engine (internal/monitorsvc) writes it; the per-NAS/per-device status
-- page reads latency/loss/CPU/mem series and the current up/down state from it.
CREATE TABLE health_probes (
    at         timestamptz NOT NULL DEFAULT now(),
    -- Exactly one of these is non-null (enforced below). Kept nullable + no FK so
    -- a probe row survives the target's deletion (history is append-only telemetry).
    nas_id     uuid,
    device_id  uuid,
    kind       text NOT NULL CHECK (kind IN ('icmp', 'snmp')),
    -- ICMP fields. latency_ms is the round-trip of the successful echo; loss is
    -- the fraction [0,1] of echoes lost in the probe's burst.
    latency_ms double precision,
    loss       double precision,
    -- SNMP fields (null on icmp rows / when community unset). uptime is sysUpTime
    -- in seconds; cpu/mem are percentages [0,100].
    cpu        double precision,
    mem        double precision,
    uptime     bigint,
    -- Probe reachability outcome; the state machine consumes this column only.
    ok         boolean NOT NULL,
    CONSTRAINT health_probes_one_target CHECK (
        (nas_id IS NOT NULL AND device_id IS NULL) OR
        (nas_id IS NULL AND device_id IS NOT NULL)
    )
);

-- Hypertable partitioned on time; 1-day chunks suit the 15 s cadence and the
-- 30-day-ish history the status page graphs.
SELECT create_hypertable('health_probes', 'at', chunk_time_interval => INTERVAL '1 day');

-- Status-page reads scan a time range for one target.
CREATE INDEX health_probes_nas_idx    ON health_probes (nas_id, at DESC)    WHERE nas_id IS NOT NULL;
CREATE INDEX health_probes_device_idx ON health_probes (device_id, at DESC) WHERE device_id IS NOT NULL;

-- Compress old chunks and cap retention: probe telemetry is high-volume and only
-- the recent window is graphed. 45 days is comfortably above the status page's
-- needs while keeping the hypertable small on the NFR-3 tier.
ALTER TABLE health_probes SET (
    timescaledb.compress,
    timescaledb.compress_orderby = 'at DESC'
);
SELECT add_compression_policy('health_probes', INTERVAL '7 days');
SELECT add_retention_policy('health_probes', INTERVAL '45 days');
