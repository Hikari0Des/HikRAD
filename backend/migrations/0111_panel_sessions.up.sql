-- 0111_panel_sessions — Phase 2, Agent 1, range 0110–0119. Contract C1-A / FR-29.
-- One row per successful manager login. The refresh token is never stored in
-- the clear: refresh_hash is sha256(refresh secret) and rotates on every
-- /auth/refresh. Reuse of a rotated-away secret revokes the whole session
-- (set revoked = true) — the row is retained for the audit trail.
CREATE TABLE panel_sessions (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    manager_id   uuid NOT NULL REFERENCES managers(id) ON DELETE CASCADE,
    refresh_hash bytea NOT NULL,
    ua           text NOT NULL DEFAULT '',
    ip           text NOT NULL DEFAULT '',
    created_at   timestamptz NOT NULL DEFAULT now(),
    last_seen_at timestamptz NOT NULL DEFAULT now(),
    revoked      boolean NOT NULL DEFAULT false
);

CREATE INDEX panel_sessions_manager_idx ON panel_sessions (manager_id, created_at DESC);
