-- v4-7 (Router manager II), FR-146. The safety net for the one change class
-- that can lock an operator out of their own router: before HikRAD applies a
-- security-relevant firewall change, it snapshots the affected rules'
-- CURRENT field values (never the whole router — only what the change is
-- about to touch) and the exact "revert" sentences that would restore them,
-- with a deadline. If the operator does not explicitly confirm within the
-- window, the firewall revert worker (module.go) applies the stored revert
-- sentences automatically — a /set back to the pre-change values, never a
-- /remove, matching the phase's "nothing is ever deleted" rule even for its
-- own safety mechanism.
--
-- revert_sentences is stored (not recomputed at revert time) deliberately:
-- recomputing "the opposite of what we just did" from router state AFTER a
-- lockout is exactly the moment the router may be unreachable in the normal
-- way (that's the failure this exists to catch), so the worker must be able
-- to act from data already in hand, not a fresh read.
--
-- One partial index over the pending set (confirmed=false, reverted=false)
-- is the revert worker's whole query — a NAS with normal firewall usage
-- accumulates rows here only across confirmed-or-reverted history, which the
-- index never has to scan.
--
-- Forward-only (FR-51.4 precedent): no paired .down.sql.

CREATE TABLE nas_firewall_snapshots (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    nas_id              uuid NOT NULL REFERENCES nas(id) ON DELETE CASCADE,
    change_description  text NOT NULL,
    snapshot            jsonb NOT NULL,
    revert_sentences    jsonb NOT NULL DEFAULT '[]'::jsonb,
    revert_deadline     timestamptz NOT NULL,
    confirmed           boolean NOT NULL DEFAULT false,
    confirmed_at        timestamptz,
    reverted            boolean NOT NULL DEFAULT false,
    revert_error        text,
    reverted_at         timestamptz,
    created_by          uuid,
    created_at          timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX nas_firewall_snapshots_nas_idx ON nas_firewall_snapshots (nas_id);

CREATE INDEX nas_firewall_snapshots_pending_idx ON nas_firewall_snapshots (revert_deadline)
    WHERE confirmed = false AND reverted = false;
