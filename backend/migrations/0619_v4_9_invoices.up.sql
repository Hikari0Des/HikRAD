-- v4-9 (FR-154): invoices. Accrual: an invoice posts its journal entry
-- (Dr Accounts Receivable / Cr Subscription Revenue) at ISSUE time, not when
-- it is later paid — the receipt (0621) is a separate settling entry
-- (Dr Cash / Cr Accounts Receivable). A no_charge invoice (total=0) posts no
-- journal entry at all — nothing changed financially — but the row itself
-- still exists for the paper trail (FR-154's "no-charge" mode is a billing
-- decision, not a data-retention one).

CREATE SEQUENCE IF NOT EXISTS invoice_seq START 1;

CREATE TABLE invoices (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    invoice_no       text NOT NULL UNIQUE,
    subscriber_id    uuid REFERENCES subscribers(id) ON DELETE SET NULL,
    mode             text NOT NULL CHECK (mode IN ('full', 'prorated', 'no_charge')),
    issue_date       date NOT NULL DEFAULT CURRENT_DATE,
    due_date         date NOT NULL,
    tax_rate         numeric(5,2) NOT NULL DEFAULT 0 CHECK (tax_rate >= 0),
    subtotal         bigint NOT NULL DEFAULT 0 CHECK (subtotal >= 0),
    tax_amount       bigint NOT NULL DEFAULT 0 CHECK (tax_amount >= 0),
    total            bigint NOT NULL DEFAULT 0 CHECK (total >= 0),
    amount_paid      bigint NOT NULL DEFAULT 0 CHECK (amount_paid >= 0),
    -- unpaid -> partial -> due -> paid (FR-156's lifecycle); recomputed
    -- server-side on every state-changing write (invoices.go's
    -- recomputeInvoiceStatus), never hand-set by the API directly except
    -- the terminal 'void'.
    status           text NOT NULL DEFAULT 'unpaid' CHECK (status IN ('unpaid', 'partial', 'due', 'paid', 'void')),
    memo             text NOT NULL DEFAULT '',
    journal_entry_id uuid REFERENCES journal_entries(id),
    created_by       uuid REFERENCES managers(id),
    created_at       timestamptz NOT NULL DEFAULT now(),
    voided_at        timestamptz,
    voided_by        uuid REFERENCES managers(id),
    void_reason      text,
    void_journal_entry_id uuid REFERENCES journal_entries(id)
);
CREATE INDEX invoices_subscriber_idx ON invoices (subscriber_id, issue_date DESC);
CREATE INDEX invoices_status_idx     ON invoices (status);

CREATE TABLE invoice_lines (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    invoice_id  uuid NOT NULL REFERENCES invoices(id) ON DELETE CASCADE,
    description text NOT NULL,
    category    text NOT NULL DEFAULT 'subscription',
    quantity    numeric(10,2) NOT NULL DEFAULT 1 CHECK (quantity > 0),
    unit_price  bigint NOT NULL CHECK (unit_price >= 0),
    amount      bigint NOT NULL CHECK (amount >= 0)
);
CREATE INDEX invoice_lines_invoice_idx ON invoice_lines (invoice_id);

-- Permission gating no-charge invoicing (SAMM's "allow no invoice" right,
-- FR-154). A dedicated permission, not folded into billing.create, because
-- an operator who may invoice normally should not automatically be able to
-- waive revenue.
