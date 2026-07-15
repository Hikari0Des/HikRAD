-- 0410_license — Phase 5, Agent 1, range 0410-0419. Contract C1-A / C4 / FR-50.
-- Single-row table (id is always 'current'): the currently installed license.
-- payload/signature are the raw uploaded blob (re-verified on every read, never
-- trusted from state alone); fingerprint is the fingerprint the license was
-- issued for, so a later fingerprint change is detectable without re-parsing
-- payload. state is the cached grace-machine result (license.Evaluate keeps it
-- current on every read that finds it stale).
CREATE TABLE license (
    id               text NOT NULL DEFAULT 'current' PRIMARY KEY,
    key_id           text NOT NULL,
    payload          jsonb NOT NULL,
    signature        text NOT NULL,
    fingerprint      text NOT NULL,
    state            text NOT NULL DEFAULT 'valid' CHECK (state IN ('valid', 'grace', 'expired_grace')),
    grace_started_at timestamptz,
    installed_at     timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT license_single_row CHECK (id = 'current')
);
