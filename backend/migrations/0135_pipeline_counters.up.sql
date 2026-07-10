-- 0135_pipeline_counters — Phase 2, Agent 3. Contract C1-C / FR-40.
-- Monotonic per-stage counters that prove the M2 zero-loss claim. Redis holds
-- the hot counters (atomically incremented on the ack path, sub-ms); this table
-- is the durable mirror flushed periodically and on shutdown so the invariant
-- survives a service restart (DoD: "counters survive service restart").
--
-- Single-row table (id=TRUE); the invariant checked on /internal/acct/counters
-- is received − deduplicated − in_queue == persisted.
CREATE TABLE pipeline_counters (
    id           boolean PRIMARY KEY DEFAULT true CHECK (id),
    received     bigint NOT NULL DEFAULT 0,  -- packets accepted at /acct
    enqueued     bigint NOT NULL DEFAULT 0,  -- durably appended to the stream
    spilled      bigint NOT NULL DEFAULT 0,  -- written to the disk WAL (Redis down)
    drained      bigint NOT NULL DEFAULT 0,  -- replayed from the WAL into the stream
    persisted    bigint NOT NULL DEFAULT 0,  -- committed to sessions/usage
    deduplicated bigint NOT NULL DEFAULT 0,  -- dropped NAS retransmits
    reaped       bigint NOT NULL DEFAULT 0,  -- sessions closed by synthesized Stop
    orphan_stops bigint NOT NULL DEFAULT 0,  -- Stop for a never-seen session
    updated_at   timestamptz NOT NULL DEFAULT now()
);

INSERT INTO pipeline_counters (id) VALUES (true) ON CONFLICT DO NOTHING;
