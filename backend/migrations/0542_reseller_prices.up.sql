-- v2 phase 9 / FR-74 (contract C4): reseller (wholesale) pricing, flat
-- 2-level only (owner-confirmed kickoff blocker, PRD Decision 36 — no
-- sub-reseller tree, so manager_id is always a direct reseller of the owner;
-- this migration adds no ancestry/parent-manager column anywhere).
--
-- subscriber_id NULL = this reseller's plan-wide wholesale price for the
-- profile; a non-null subscriber_id overrides it for that one subscriber
-- only. No row at all for a given (manager_id, profile_id) falls back to the
-- plan's retail price — v1's exact behavior, so every pre-v2-9 agent is
-- unaffected on day one.
--
-- Append-only versioned, same posture as profile_cost_history/currency_rates:
-- a price change is a new row, never an UPDATE, so a past renewal's resolved
-- wholesale price is always independently re-derivable from history.

CREATE TABLE reseller_prices (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    manager_id     uuid NOT NULL REFERENCES managers(id) ON DELETE CASCADE,
    profile_id     uuid NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
    subscriber_id  uuid REFERENCES subscribers(id) ON DELETE CASCADE,
    price          bigint NOT NULL,
    currency       text NOT NULL REFERENCES currencies(code),
    effective_from timestamptz NOT NULL DEFAULT now(),
    created_by     uuid REFERENCES managers(id),
    created_at     timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX reseller_prices_lookup_idx ON reseller_prices (manager_id, profile_id, subscriber_id, effective_from DESC);
