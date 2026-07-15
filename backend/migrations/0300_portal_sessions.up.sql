-- 0300_portal_sessions — Phase 4, Agent 3 (Backend Business), range 0300–0309.
-- Contract C1-D / C2. Subscriber refresh-token sessions, mirroring panel_sessions
-- (0111) but kept in a separate table so a portal session can never be mistaken
-- for a manager one and portal-side revocation never touches panel_sessions.
-- Refresh tokens are opaque `<id>.<secret>`; only sha256(secret) is stored and
-- it rotates on every refresh (token-theft detection, same discipline as auth).

CREATE TABLE portal_sessions (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    subscriber_id uuid NOT NULL REFERENCES subscribers(id) ON DELETE CASCADE,
    refresh_hash  bytea NOT NULL,
    ip            text NOT NULL DEFAULT '',
    ua            text NOT NULL DEFAULT '',
    revoked_at    timestamptz,
    created_at    timestamptz NOT NULL DEFAULT now(),
    rotated_at    timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX portal_sessions_subscriber_idx ON portal_sessions (subscriber_id, created_at DESC);
