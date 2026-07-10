-- 0110_managers_security — Phase 2, Agent 1 (Platform & Security), range 0110–0119.
-- Contract C1-A: managers += TOTP fields (unused until Phase 3) + scoped flag.
-- FR-27.2 (ownership scoping), FR-28.1 (TOTP seam). Additive only; the flat
-- v1 structure (FR-27.4) is preserved — no manager-under-manager relations.
ALTER TABLE managers
    ADD COLUMN IF NOT EXISTS scoped          boolean NOT NULL DEFAULT false,
    -- TOTP secret is AES-GCM encrypted at rest via platform/crypto (NFR-4.3);
    -- Phase 3 wires enrolment/verification. Nullable + disabled by default now.
    ADD COLUMN IF NOT EXISTS totp_secret_enc bytea,
    ADD COLUMN IF NOT EXISTS totp_enabled    boolean NOT NULL DEFAULT false;
