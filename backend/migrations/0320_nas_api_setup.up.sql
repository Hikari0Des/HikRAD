-- 0320_nas_api_setup — Phase 4, Agent B (RADIUS & NAS), range 0320-0329.
-- Contract C1-B / C6, FR-56.2 amendment 2026-07-09: RouterOS API auto-setup
-- credentials. Sealed with A's crypto (NFR-4) exactly like secret_enc /
-- snmp_community_enc — write-only after save, reveal permission-gated per
-- FR-13.3. api_password_enc is nullable: a NAS with no credentials saved
-- falls back to the FR-14 copy-paste snippet (FR-56.3), it is never required.
ALTER TABLE nas
    ADD COLUMN api_port         int  NOT NULL DEFAULT 8728 CHECK (api_port BETWEEN 1 AND 65535),
    ADD COLUMN api_user         text,
    ADD COLUMN api_password_enc bytea;
