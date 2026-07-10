-- 0130_sessions — Phase 2, Agent 3 (Accounting & Monitoring), migration range
-- 0130–0139. Contract C1-C: the live/historical session record the lossless
-- pipeline upserts (Start creates, Interim updates, Stop closes). One row per
-- (nas_id, acct_session_id); dedup of individual packets is handled separately
-- by acct_dedup (0134).
--
-- TimescaleDB is the platform DB (compose: timescale/timescaledb-pg16); the
-- usage_points hypertable (0131) and usage_daily continuous aggregate (0132)
-- need the extension, so create it here in the first C-owned migration.
CREATE EXTENSION IF NOT EXISTS timescaledb;

CREATE TABLE sessions (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    -- The NAS this session lives on. Read-only FK-in-spirit to B's nas table;
    -- not a hard FK because accounting must record a session even for a NAS the
    -- registry has not caught up on (orphan tolerance, edge cases in the brief).
    nas_id           uuid,
    acct_session_id  text NOT NULL,
    -- Resolved from username at ingest; NULL when no subscriber matches (voucher
    -- / unknown login). Sessions are still recorded — never dropped.
    subscriber_id    uuid,
    username         text NOT NULL DEFAULT '',
    ip               inet,
    mac              text NOT NULL DEFAULT '',
    started_at       timestamptz,
    last_interim_at  timestamptz,
    stopped_at       timestamptz,
    terminate_cause  text NOT NULL DEFAULT '',
    -- Total bytes as of the latest record, reconstructed from 32-bit octets +
    -- gigawords with wrap/reset handling in the consumer (FR-37.2).
    bytes_in         bigint NOT NULL DEFAULT 0,
    bytes_out        bigint NOT NULL DEFAULT 0,
    packets          bigint NOT NULL DEFAULT 0,
    -- FR-38: reaper lifecycle flags. stale = missed interims; reaped = closed by
    -- a synthesized Stop. Both are surfaced in history (never silently deleted).
    stale            boolean NOT NULL DEFAULT false,
    reaped           boolean NOT NULL DEFAULT false,
    -- Derived from nas.type per amended C1-C (FR-58). Quota math excludes
    -- service='hotspot' rows; graphs/reports include them.
    service          text NOT NULL DEFAULT 'pppoe'
                       CHECK (service IN ('pppoe', 'hotspot')),
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now(),
    -- One logical session per NAS + Acct-Session-Id (FR-37.2 upsert key).
    UNIQUE (nas_id, acct_session_id)
);

-- Live table + history queries filter by subscriber, by NAS, and by open/closed.
CREATE INDEX sessions_subscriber_idx ON sessions (subscriber_id, started_at DESC);
CREATE INDEX sessions_nas_idx        ON sessions (nas_id);
-- Partial index over still-open sessions: the reaper scans these every tick.
CREATE INDEX sessions_open_idx       ON sessions (last_interim_at) WHERE stopped_at IS NULL;

-- Namespaced touch-updated_at (own migration range; cannot collide with another
-- agent's identically-named function).
CREATE OR REPLACE FUNCTION acct_touch_updated_at() RETURNS trigger AS $$
BEGIN
    NEW.updated_at := now();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER sessions_set_updated_at
    BEFORE UPDATE ON sessions
    FOR EACH ROW EXECUTE FUNCTION acct_touch_updated_at();
