-- Instance-level default payment methods (owner decision 2026-07-19, amending
-- v2-2's Decision 37): a subscriber with NO owning manager previously resolved
-- an empty Pay screen — enabling a method or adding a provider appeared to do
-- nothing (docs/ops/known-issues.md). These instance defaults are used ONLY
-- when owner_manager_id IS NULL; an owned subscriber still resolves solely
-- from their owner's rows, so no cross-manager fallback is introduced.
CREATE TABLE instance_method_settings (
    method_key text PRIMARY KEY,
    enabled    boolean NOT NULL DEFAULT false
);

CREATE TABLE instance_provider_accounts (
    provider_id           uuid PRIMARY KEY REFERENCES payment_providers(id) ON DELETE CASCADE,
    account_details       text NOT NULL,
    instructions_override text,
    created_at            timestamptz NOT NULL DEFAULT now(),
    updated_at            timestamptz NOT NULL DEFAULT now()
);
