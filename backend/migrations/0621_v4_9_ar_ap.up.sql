-- v4-9 (FR-155): receipts (money in, settling an invoice) and payments
-- (money out, settling an expense). Named ar_receipts/ap_payments —
-- deliberately NOT reusing the existing `payments` table (that one is the
-- FR-21 subscriber-facing receipt behind a renewal/voucher/card charge, a
-- different concept the phase brief's §4 freezes unchanged) so the two
-- money-in tables never collide or need disambiguating by a discriminator
-- column.
--
-- Both are append-only, same discipline as `payments` (AC-24a's pattern):
-- once money has moved, the record does not get edited, only ever
-- referenced by later rows.

CREATE SEQUENCE IF NOT EXISTS ar_receipt_seq START 1;
CREATE SEQUENCE IF NOT EXISTS ap_payment_seq  START 1;

CREATE TABLE ar_receipts (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    receipt_no       text NOT NULL UNIQUE,
    invoice_id       uuid NOT NULL REFERENCES invoices(id),
    amount           bigint NOT NULL CHECK (amount > 0),
    method           text NOT NULL DEFAULT 'cash',
    received_at      timestamptz NOT NULL DEFAULT now(),
    memo             text NOT NULL DEFAULT '',
    journal_entry_id uuid REFERENCES journal_entries(id),
    created_by       uuid REFERENCES managers(id)
);
CREATE INDEX ar_receipts_invoice_idx ON ar_receipts (invoice_id);

CREATE TABLE ap_payments (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    payment_no       text NOT NULL UNIQUE,
    expense_id       uuid NOT NULL REFERENCES expenses(id),
    amount           bigint NOT NULL CHECK (amount > 0),
    method           text NOT NULL DEFAULT 'cash',
    paid_at          timestamptz NOT NULL DEFAULT now(),
    memo             text NOT NULL DEFAULT '',
    journal_entry_id uuid REFERENCES journal_entries(id),
    created_by       uuid REFERENCES managers(id)
);
CREATE INDEX ap_payments_expense_idx ON ap_payments (expense_id);

CREATE OR REPLACE FUNCTION ar_ap_immutable() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION '% is append-only: % is not permitted', TG_TABLE_NAME, TG_OP
        USING ERRCODE = 'restrict_violation';
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER ar_receipts_no_update BEFORE UPDATE ON ar_receipts FOR EACH ROW EXECUTE FUNCTION ar_ap_immutable();
CREATE TRIGGER ar_receipts_no_delete BEFORE DELETE ON ar_receipts FOR EACH ROW EXECUTE FUNCTION ar_ap_immutable();
CREATE TRIGGER ap_payments_no_update BEFORE UPDATE ON ap_payments FOR EACH ROW EXECUTE FUNCTION ar_ap_immutable();
CREATE TRIGGER ap_payments_no_delete BEFORE DELETE ON ap_payments FOR EACH ROW EXECUTE FUNCTION ar_ap_immutable();
REVOKE UPDATE, DELETE ON ar_receipts, ap_payments FROM PUBLIC;
