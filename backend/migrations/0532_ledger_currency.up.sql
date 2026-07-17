-- v2 phase 4 / FR-68.2, FR-69.3 (contract C2): ledger_transactions gains a
-- currency and threads it end to end; amount_iqd is renamed to amount
-- (semantics: minor units of `currency`, not always IQD anymore) — every
-- pre-migration row backfills to currency='IQD' with its stored integer
-- UNCHANGED, because IQD's minor_unit_digits=0 means the rename changes no
-- value. type gains 'exchange' (FR-69.3: the only currency-conversion path —
-- two linked rows, from_currency debit + to_currency credit, sharing a
-- reference and stamping the currency_rates row used via currency_rate_id).

ALTER TABLE ledger_transactions RENAME COLUMN amount_iqd TO amount;

ALTER TABLE ledger_transactions
    ADD COLUMN currency text NOT NULL DEFAULT 'IQD' REFERENCES currencies(code),
    ADD COLUMN currency_rate_id uuid REFERENCES currency_rates(id);

ALTER TABLE ledger_transactions DROP CONSTRAINT ledger_transactions_type_check;
ALTER TABLE ledger_transactions ADD CONSTRAINT ledger_transactions_type_check
    CHECK (type IN ('renewal','topup','manual_payment','voucher_redeem','refund','adjustment','discount','exchange'));
