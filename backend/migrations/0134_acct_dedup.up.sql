-- 0134_acct_dedup — Phase 2, Agent 3. Contract C1-C / FR-37.4.
-- The idempotency ledger. The consumer inserts one row per accounting packet
-- keyed by (nas_id, acct_session_id, record_type, event_time); a unique-
-- violation on insert means the packet is a NAS retransmit — counted as a
-- duplicate (FR-40) and dropped. All four columns form the key (amended C1-C:
-- "unique all four").
--
-- nas_id is NOT NULL (PK): the consumer substitutes the all-zero UUID sentinel
-- for a packet whose source IP is not a registered NAS, so the key — and the
-- sessions/usage_points upserts — always compare concrete values rather than
-- SQL-distinct NULLs.
CREATE TABLE acct_dedup (
    nas_id          uuid        NOT NULL,
    acct_session_id text        NOT NULL,
    record_type     text        NOT NULL,
    event_time      timestamptz NOT NULL,
    seen_at         timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (nas_id, acct_session_id, record_type, event_time)
);
