-- 0213_totp_backup_codes — Phase 3, Agent 1, range 0210–0219. FR-28.1.
-- Completes TOTP 2FA on top of the Phase-2 seam (managers.totp_secret_enc /
-- totp_enabled, migration 0110). A pending secret holds an enrolment that has
-- not yet been verified; on verify it is promoted to the active secret and
-- totp_enabled flips true. Backup codes are one-time, stored only as a hash.
ALTER TABLE managers
    ADD COLUMN IF NOT EXISTS totp_pending_secret_enc bytea,
    ADD COLUMN IF NOT EXISTS totp_enrolled_at        timestamptz;

CREATE TABLE manager_backup_codes (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    manager_id uuid NOT NULL REFERENCES managers(id) ON DELETE CASCADE,
    -- sha256 of a high-entropy random code (same discipline as refresh
    -- secrets); a used code is marked, never deleted, so reuse is auditable.
    code_hash  bytea NOT NULL,
    used_at    timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX manager_backup_codes_mgr_idx ON manager_backup_codes (manager_id);
