-- v2-2 / FR-77.3 (contract C3): per-manager toggle for every payment method
-- their subscribers may use. method_key is either a payment_providers.id
-- (as text) or one of the two literal built-in keys 'scratch_card' /
-- 'voucher' — enforced in application code, not a DB FK, since a provider
-- row and a built-in key live in different id-spaces and a CHECK constraint
-- cannot conditionally reference a foreign table. No row for a given
-- (manager_id, method_key) means enabled=false — the same "absence is off"
-- default every other settings-shaped table in this codebase already uses.

CREATE TABLE manager_method_settings (
    manager_id  uuid NOT NULL REFERENCES managers(id) ON DELETE CASCADE,
    method_key  text NOT NULL,
    enabled     boolean NOT NULL DEFAULT false,
    PRIMARY KEY (manager_id, method_key)
);
