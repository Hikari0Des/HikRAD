-- 0304_card_payments — Phase 4, Agent 3, range 0300–0309.
-- Contract C8, FR-59 (amendment 2026-07-11). Telecom scratch-card submissions:
-- a subscriber submits a card code and immediately gets a 1-day provisional
-- renewal (trial_ledger_tx_id) while the card sits in a manual verification
-- queue; an admin approves (full renewal, approve_ledger_tx_id) or rejects
-- (reversing entry, same trial_ledger_tx_id reversed). card_code_enc is sealed
-- with A's crypto and must never appear in a list/query response — only the
-- explicit, audited /reveal action decrypts it.
--
-- The partial unique index is the DB-level abuse guard (FR-59.4): at most one
-- pending card payment per subscriber, race-proof by construction (a second
-- concurrent INSERT while one is pending hits 23505, no application-level
-- check-then-insert race is possible).
CREATE TABLE card_payments (
    id                   uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    subscriber_id        uuid NOT NULL REFERENCES subscribers(id),
    profile_id           uuid NOT NULL REFERENCES profiles(id),
    card_type            text NOT NULL,
    card_code_enc        bytea NOT NULL,
    state                text NOT NULL DEFAULT 'pending', -- pending|approved|rejected
    trial_started_at     timestamptz NOT NULL DEFAULT now(), -- approve anchors here (FR-59.2: trial day included, not added)
    trial_ledger_tx_id   uuid REFERENCES ledger_transactions(id),
    approve_ledger_tx_id uuid REFERENCES ledger_transactions(id),
    decided_by           uuid,
    decided_at           timestamptz,
    reject_reason        text,
    created_at           timestamptz NOT NULL DEFAULT now(),
    updated_at           timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX card_payments_one_pending_idx ON card_payments (subscriber_id) WHERE state = 'pending';
CREATE INDEX card_payments_subscriber_idx ON card_payments (subscriber_id, created_at DESC);
CREATE INDEX card_payments_state_idx ON card_payments (state, created_at DESC);
