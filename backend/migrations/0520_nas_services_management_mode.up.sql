-- v2 phase 2 / FR-67 (contract C5, Decision 31): every nas_services instance
-- carries a management mode. 'router' means HikRAD discovered or was told
-- about a service instance the router already had (v2 phase 1's migration
-- 0501 backfill, manual entry, or FR-62.6 discovery/merge) — HikRAD can read
-- its config but never writes to it until explicitly adopted. 'system' means
-- HikRAD itself provisioned the instance (FR-67.3) or the operator explicitly
-- adopted a router-managed one (FR-67.5) — only then can HikRAD edit it.
--
-- Every row that exists before this migration was created by discovery,
-- backfill, or manual entry — never by HikRAD provisioning a server on the
-- router — so 'router' is the correct default for all of them without a
-- special-case backfill query.
--
-- Forward-only (FR-51.4): no paired .down.sql.

ALTER TABLE nas_services
    ADD COLUMN management_mode text NOT NULL DEFAULT 'router'
        CHECK (management_mode IN ('router', 'system'));
