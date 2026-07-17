-- v2 phase 9 / FR-73 (contract C3): fixed periodic overheads (uplink, staff,
-- power, rent), entered by an admin, never derived. Optional nas_id scopes an
-- overhead to one site/tower; NULL is a whole-business (global) overhead.
--
-- Overheads are reported separately from per-plan gross margin and never
-- allocated onto it by default (every allocation rule is arguable — see the
-- phase brief C3). A per-site net-margin report nets ONLY that NAS's own
-- tagged rows against that NAS's own revenue; it never pro-rates a share of
-- the global (nas_id IS NULL) rows onto a site.
--
-- Edits happen by superseding (set the old row's period_end, insert a new
-- row) rather than an in-place UPDATE of amount — so a past period's already-
-- reported net margin never silently changes.

CREATE TABLE overheads (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name         text NOT NULL,
    amount       bigint NOT NULL,
    currency     text NOT NULL REFERENCES currencies(code),
    nas_id       uuid REFERENCES nas(id) ON DELETE SET NULL,
    period_start timestamptz NOT NULL,
    period_end   timestamptz,
    notes        text NOT NULL DEFAULT '',
    created_by   uuid REFERENCES managers(id),
    created_at   timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX overheads_nas_period_idx ON overheads (nas_id, period_start);
