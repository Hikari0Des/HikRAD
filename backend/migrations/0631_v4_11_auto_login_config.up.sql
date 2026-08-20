-- 0631 — v4-11 (Connected-client portal auto-login), FR-167, Decision 64.
-- Two things:
--
-- 1. portal_sessions gains scope/expires_at (a reduced-scope, hard-capped
--    session — see backend/internal/portalapi/autologin.go and tokens.go).
--    Every EXISTING row backfills to ('full', NULL): unbounded lifetime,
--    exactly its pre-v4-11 behavior — this migration changes nothing about
--    any session that already exists.
--
-- 2. The auto-login config seeds into the existing generic settings table
--    (0010) under the "portal_auto_login" group (wired into
--    internal/platform/setupapi's settingsGroups this same phase) rather
--    than a bespoke table — it is exactly the same shape of per-instance
--    config every notifications.* key already is, so it reuses that proven
--    read/write/audit path (GET/PUT /api/v1/settings/portal_auto_login,
--    settings.edit permission) instead of a second config surface. Gate
--    item 4 ("off by default") is enforced by this INSERT: a fresh install
--    has enabled=false and empty CIDR lists before any operator ever opens
--    the settings page.
ALTER TABLE portal_sessions
    ADD COLUMN scope      text NOT NULL DEFAULT 'full' CHECK (scope IN ('full','reduced')),
    ADD COLUMN expires_at timestamptz;

INSERT INTO settings (key, value) VALUES
    ('portal_auto_login.enabled',             'false'),
    ('portal_auto_login.trusted_networks',    '[]'),
    ('portal_auto_login.trusted_proxies',     '[]'),
    ('portal_auto_login.session_ttl_minutes', '30')
ON CONFLICT (key) DO NOTHING;
