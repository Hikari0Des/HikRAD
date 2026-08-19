-- 0615 — v4-8 (CoA outbox & dispatch), FR-150/FR-151.
--
-- coa_attempts: one row per dispatch attempt the worker tick makes against a
-- coa_outbox row, so "why did this fail" is answerable from the DB after the
-- fact rather than from logs that have long since rotated (phase brief §3.1).
-- wire_attributes is captured at the moment of the real UDP exchange
-- (coa_wire.go's dump of the constructed layeh radius.Packet, generic/
-- vendor-neutral byte decode — never a hardcoded vendor attribute name, so
-- this does not violate FR-17 vendor isolation) — this is the literal
-- evidence gate item 6 checks against a packet capture, stored once and never
-- recomputed at display time.
CREATE TABLE coa_attempts (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    outbox_id       uuid NOT NULL REFERENCES coa_outbox(id) ON DELETE CASCADE,
    attempt_no      int  NOT NULL,
    op              text NOT NULL CHECK (op IN ('update', 'disconnect')),
    outcome         text NOT NULL, -- ack | nak | timeout | error | unsupported | skipped_reaped
    error           text NOT NULL DEFAULT '',
    wire_attributes jsonb NOT NULL DEFAULT '[]',
    attempted_at    timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX coa_attempts_outbox_idx ON coa_attempts (outbox_id, attempt_no);

-- Retry limit and dispatch timeout (FR-151 §3.2: "come from the existing
-- hot-reloadable settings", i.e. this settings table/service — not a new
-- config mechanism). coa_port stays per-NAS (nas.coa_port, already existing)
-- since a retry/timeout budget is process-wide but a CoA port is a per-router
-- fact. ON CONFLICT DO NOTHING mirrors 0010's seeding convention so a
-- pre-existing install with these keys already set (never possible before
-- this migration, but future-proof) is left untouched.
INSERT INTO settings (key, value) VALUES
    ('radius.coa_retry_max',       '3'),
    ('radius.coa_timeout_seconds', '5')
ON CONFLICT (key) DO NOTHING;
