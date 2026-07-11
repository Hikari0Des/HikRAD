-- 0202_vouchers — Phase 3, Agent 4, range 0200–0209.
-- Contract C1-D / C3 / sub-PRD 05 FR-22. One-time voucher batches. Charging model
-- is FROZEN at generation (C3): batch creation debits the creator's balance;
-- void-batch of unused codes credits it back. Codes are stored only as a hash —
-- plaintext exists solely in the generation-time CSV response (FR-22.1).
CREATE TABLE voucher_batches (
    id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    profile_id         uuid NOT NULL REFERENCES profiles(id) ON DELETE RESTRICT,
    prefix             text NOT NULL DEFAULT '',
    count              int  NOT NULL CHECK (count > 0 AND count <= 10000),
    unit_price_iqd     bigint NOT NULL,             -- resolved profile price per code at generation
    creator_manager_id uuid REFERENCES managers(id) ON DELETE SET NULL,
    expires_at         timestamptz,                 -- code expiry (null = never)
    state              text NOT NULL DEFAULT 'active' CHECK (state IN ('active','void')),
    -- the debit posted to the creator's balance at generation (FR-22.3), and (on
    -- void) the reversing credit for the unused remainder.
    gen_ledger_tx_id   uuid REFERENCES ledger_transactions(id),
    void_ledger_tx_id  uuid REFERENCES ledger_transactions(id),
    created_at         timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE vouchers (
    id                     uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    batch_id               uuid NOT NULL REFERENCES voucher_batches(id) ON DELETE CASCADE,
    -- sha256 hex of the (upper-cased) plaintext code. Never store plaintext at
    -- rest (FR-22.1); redemption hashes the submitted code and looks it up.
    code_hash              text NOT NULL UNIQUE,
    state                  text NOT NULL DEFAULT 'unused' CHECK (state IN ('unused','used','void')),
    used_by_manager_id     uuid REFERENCES managers(id) ON DELETE SET NULL,
    used_for_subscriber_id uuid REFERENCES subscribers(id) ON DELETE SET NULL,
    used_at                timestamptz,
    redeem_ledger_tx_id    uuid REFERENCES ledger_transactions(id)
);
CREATE INDEX vouchers_batch_idx ON vouchers (batch_id, state);
