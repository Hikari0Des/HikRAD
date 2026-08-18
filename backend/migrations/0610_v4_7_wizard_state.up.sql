-- v4-7 (Router manager II), FR-144. Migration-number note: the phase brief
-- budgeted 0609-0612, but v4-6 (this repo's immediately preceding phase) was
-- itself renumbered 0607-0609 at ITS kickoff (see 0607's note), leaving 0609
-- as the real repo max at v4-7's kickoff. Per the master plan's linear-
-- sequence rule (budgets are not reservations), v4-7 starts at the next free
-- number, 0610, not its stated 0609.
--
-- nas_setup_wizard_state: one row per NAS holding the 10-step wizard's
-- current step and every step's form values so far, as one JSON blob keyed
-- by step name (bridge/dns/ntp/wan/radius/services/firewall/ipv6 — whichever
-- steps the operator has reached). This is what makes "Back works at every
-- step" and "resume after a browser close" (gate items 3/4) more than a
-- client-side-only guarantee: the panel PUTs this row after every step
-- transition (including Back, which only ever writes `step`, never discards
-- a later step's already-entered values), and GETs it on wizard open to
-- resume exactly where the operator left off. completed marks a finished
-- run so the panel can offer "re-run" (detect-and-skip, gate item 2) instead
-- of silently reopening a stale in-progress state.
--
-- Deliberately NOT where the review step's plan/apply hash lives — that is
-- recomputed fresh from router state on every preview/apply call, same as
-- every other write in this codebase (planHash's whole reason to exist is
-- that nothing is trusted from a stored session).
--
-- Forward-only (FR-51.4 precedent): no paired .down.sql.

CREATE TABLE nas_setup_wizard_state (
    nas_id     uuid PRIMARY KEY REFERENCES nas(id) ON DELETE CASCADE,
    step       int NOT NULL DEFAULT 1 CHECK (step BETWEEN 1 AND 10),
    form_data  jsonb NOT NULL DEFAULT '{}'::jsonb,
    completed  boolean NOT NULL DEFAULT false,
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TRIGGER nas_setup_wizard_state_set_updated_at
    BEFORE UPDATE ON nas_setup_wizard_state
    FOR EACH ROW EXECUTE FUNCTION radius_touch_updated_at();
