-- 0505_manager_disabled — v1.x maintenance (owner request 2026-07-17, manager
-- removal). Numbered 0505, NOT 04xx: golang-migrate is strictly linear and the
-- fleet is already at 0504 (v2 phase 1) — a 04xx file would silently never run
-- on updated installs (see docs/ops/known-issues.md, 2026-07-17 migrations row).
-- A disabled manager cannot log in, refresh, or hold live sessions.
-- Hard delete exists too, but a manager with ledger history can never be
-- hard-deleted: ledger_transactions' append-only trigger (0200) rejects the
-- FK's ON DELETE SET NULL update — so disabling is that manager's terminal
-- state, and the API maps the trigger error to a "disable instead" conflict.
ALTER TABLE managers ADD COLUMN IF NOT EXISTS disabled boolean NOT NULL DEFAULT false;
