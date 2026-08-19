-- v4-9 (FR-155): expenses. Accrual: recording an expense as "incurred"
-- posts Dr Operating Expenses / Cr Accounts Payable immediately (the bill is
-- owed even before it is paid); an AP payment (0621) later settles it
-- (Dr Accounts Payable / Cr Cash).

CREATE SEQUENCE IF NOT EXISTS expense_seq START 1;

CREATE TABLE expenses (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    expense_no       text NOT NULL UNIQUE,
    category         text NOT NULL,
    vendor           text NOT NULL DEFAULT '',
    amount           bigint NOT NULL CHECK (amount > 0),
    amount_paid      bigint NOT NULL DEFAULT 0 CHECK (amount_paid >= 0),
    incurred_date    date NOT NULL DEFAULT CURRENT_DATE,
    due_date         date NOT NULL,
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
CREATE INDEX expenses_status_idx ON expenses (status);
