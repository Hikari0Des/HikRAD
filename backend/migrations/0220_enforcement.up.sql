-- 0220_enforcement — Phase 3, Agent 2 (RADIUS & NAS), range 0220–0229.
-- Contract C4 / FR-9,FR-10 runtime enforcement. Durable state for the
-- enforcement worker (enforce.go): it consumes the frozen Redis pub/sub
-- channels enforce.quota_exceeded / enforce.expired, resolves the profile's
-- configured behavior via D's AuthView, and executes the matching CoA on every
-- live session within one cycle (≤ 5 min).
--
-- This table is BOTH the idempotency cursor and the per-action record:
--   * dedup_key is UNIQUE, so a re-delivered event (pub/sub reconnect, or C
--     re-publishing quota_exceeded on every interim while quota stays over)
--     inserts once and the worker skips the rest — idempotency survives a
--     process restart because the guard lives in Postgres, not just Redis.
--   * the row is the audit/observability trail (which behavior ran, how many
--     sessions, how many CoA failures) that C's health surfaces alongside the
--     enforcement_failures Redis counter.
--
-- dedup_key convention: "<kind>:<subscriber_id>:<unix/300>" — a 5-minute bucket,
-- so enforcement re-runs at most once per cycle per subscriber per event kind
-- (matching the ≤ 5 min enforcement guarantee) yet a genuinely new crossing in a
-- later cycle is not suppressed.
CREATE TABLE enforcement_actions (
    id            bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    at            timestamptz NOT NULL DEFAULT now(),
    subscriber_id uuid NOT NULL,
    event_kind    text NOT NULL CHECK (event_kind IN ('quota_exceeded', 'expired', 'tod')),
    dedup_key     text NOT NULL,
    behavior      text NOT NULL DEFAULT '',   -- block | throttle | expired_pool | boost | normal
    sessions      int  NOT NULL DEFAULT 0,     -- live sessions targeted
    applied       int  NOT NULL DEFAULT 0,     -- CoA ACKed (incl. NAK→disconnect fallback that ACKed)
    failures      int  NOT NULL DEFAULT 0,     -- CoA that ended NAK/timeout/error after fallback
    outcome       text NOT NULL DEFAULT '',    -- applied | partial | failed | no_sessions | not_found
    detail        jsonb NOT NULL DEFAULT '{}'::jsonb
);

-- Idempotency guard: one enforcement row per (kind, subscriber, 5-min bucket).
CREATE UNIQUE INDEX enforcement_actions_dedup_idx ON enforcement_actions (dedup_key);

-- History lookups per subscriber (user-page enforcement timeline, debugging).
CREATE INDEX enforcement_actions_sub_idx ON enforcement_actions (subscriber_id, at DESC);
