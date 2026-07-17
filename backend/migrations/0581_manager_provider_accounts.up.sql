-- v2-2 / FR-77.2 (contract C2): per-manager receiving-account details for a
-- provider — the exact account number/phone/IBAN/recipient name shown to
-- THAT manager's subscribers. One row per (manager, provider), edited in
-- place (current-state fact, not a versioned ledger-adjacent figure). No
-- encryption at rest: these are deliberately subscriber-visible, unlike NAS
-- secrets or scratch-card codes.

CREATE TABLE manager_provider_accounts (
    id                   uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    manager_id           uuid NOT NULL REFERENCES managers(id) ON DELETE CASCADE,
    provider_id          uuid NOT NULL REFERENCES payment_providers(id) ON DELETE CASCADE,
    account_details      text NOT NULL,
    instructions_override text,
    created_at           timestamptz NOT NULL DEFAULT now(),
    updated_at           timestamptz NOT NULL DEFAULT now(),
    UNIQUE (manager_id, provider_id)
);
