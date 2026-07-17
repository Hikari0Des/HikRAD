-- v2 phase 9 / FR-71 (contract C1): versioned plan cost price. There is
-- deliberately NO cost_price/cost_currency column on profiles — the "current
-- cost" is resolved the same way v2-4's currency_rates resolves "the rate to
-- use": a query for the latest effective_from <= the moment in question,
-- never a mirrored column that could drift out of sync with its own history.
--
-- A profile with zero rows here has UNKNOWN cost (not zero) — margin
-- reporting must treat "no matching row" as its unknown state, never
-- default a missing cost to 0 (that would silently claim 100% margin).
--
-- Append-only from the API's perspective, same posture as currency_rates
-- (migration 0531): a cost change is a new row with effective_from=now(),
-- never an UPDATE, so a past renewal's stamped cost_at_sale (migration 0543)
-- is always independently re-derivable from history.

CREATE TABLE profile_cost_history (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    profile_id     uuid NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
    cost           bigint NOT NULL,
    currency       text NOT NULL REFERENCES currencies(code),
    effective_from timestamptz NOT NULL DEFAULT now(),
    created_by     uuid REFERENCES managers(id),
    created_at     timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX profile_cost_history_profile_idx ON profile_cost_history (profile_id, effective_from DESC);
