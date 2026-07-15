-- 0302_payment_intents — Phase 4, Agent 3, range 0300–0309.
-- Contract C1-D / C3, FR-23. One row per e-wallet payment attempt. State
-- machine: pending -> confirmed -> renewed (terminal success), or
-- pending/confirmed -> failed | expired (terminal failure). gateway_ref is the
-- gateway's own transaction/session reference, unique per gateway so a
-- replayed callback resolves to exactly one row; the intent's own id is passed
-- to the gateway as the merchant order id for correlation on the way back.
-- Idempotency against double-renewal is enforced in code via the existing
-- renewal_idempotency table (0204), keyed on the intent id — not by state alone
-- — so concurrent replays/races can never double-renew (C3).
CREATE TABLE payment_intents (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    subscriber_id uuid NOT NULL REFERENCES subscribers(id),
    profile_id    uuid NOT NULL REFERENCES profiles(id),
    gateway       text NOT NULL,
    amount_iqd    bigint NOT NULL,
    state         text NOT NULL DEFAULT 'pending', -- pending|confirmed|renewed|failed|expired
    gateway_ref   text,
    ledger_tx_id  uuid REFERENCES ledger_transactions(id),
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),
    last_query_at timestamptz
);

CREATE UNIQUE INDEX payment_intents_gateway_ref_idx
    ON payment_intents (gateway, gateway_ref) WHERE gateway_ref IS NOT NULL;
CREATE INDEX payment_intents_subscriber_idx ON payment_intents (subscriber_id, created_at DESC);
-- Reconciliation worker's polling set (C3: QueryStatus for pending > 10 min,
-- then hourly, expiring after 48 h).
CREATE INDEX payment_intents_open_idx ON payment_intents (state, created_at)
    WHERE state IN ('pending', 'confirmed');
