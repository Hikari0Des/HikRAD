-- v2-2 / FR-77-79 (contract C5/C7): the unified payment ticket, generalizing
-- card_payments (migration 0304) to every method. method_detail carries the
-- one field set that differs per discriminator (today: scratch cards' card
-- type + encrypted code) rather than bolting nullable card-only columns
-- onto a table most rows will never use — the standard shape for "one field
-- set differs by discriminator, everything else is shared."
--
-- Same partial-unique-one-pending-per-subscriber pattern card_payments
-- already had. No denormalized owner_manager_id column: ownership is
-- resolved through subscribers.owner_manager_id via the existing FK,
-- consistent with how every other scoped list in this codebase resolves
-- ownership rather than caching it onto the child row.

CREATE TABLE payment_tickets (
    id                   uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    subscriber_id        uuid NOT NULL REFERENCES subscribers(id),
    profile_id           uuid NOT NULL REFERENCES profiles(id),
    method_key           text NOT NULL,
    provider_id          uuid REFERENCES payment_providers(id),
    amount               bigint NOT NULL,
    currency             text NOT NULL REFERENCES currencies(code),
    transfer_reference   text,
    transfer_date        timestamptz,
    note                 text NOT NULL DEFAULT '',
    method_detail        jsonb NOT NULL DEFAULT '{}',
    state                text NOT NULL DEFAULT 'pending' CHECK (state IN ('pending','approved','rejected')),
    trial_ledger_tx_id   uuid REFERENCES ledger_transactions(id),
    approve_ledger_tx_id uuid REFERENCES ledger_transactions(id),
    decided_by           uuid,
    decided_at           timestamptz,
    reject_reason        text,
    created_at           timestamptz NOT NULL DEFAULT now(),
    updated_at           timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX payment_tickets_one_pending_idx ON payment_tickets (subscriber_id) WHERE state = 'pending';
CREATE INDEX payment_tickets_subscriber_idx ON payment_tickets (subscriber_id, created_at DESC);
CREATE INDEX payment_tickets_state_idx ON payment_tickets (state, created_at DESC);
