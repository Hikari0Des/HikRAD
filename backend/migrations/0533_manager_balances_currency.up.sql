-- v2 phase 4 / FR-69.2 (contract C3): manager balances become PER-CURRENCY.
-- A manager can hold an IQD balance and a USD balance simultaneously; FR-20.1's
-- derived-from-the-ledger rule now applies PER currency
-- (balance(M,C) = sum(ledger.amount WHERE actor_manager_id=M AND currency=C))
-- — never summed across C. Every pre-migration row keeps its exact balance
-- value under currency='IQD'; the old single-column PK becomes composite so a
-- manager can hold >1 currency row.

ALTER TABLE manager_balances RENAME COLUMN balance_iqd TO balance;
ALTER TABLE manager_balances ADD COLUMN currency text NOT NULL DEFAULT 'IQD' REFERENCES currencies(code);

ALTER TABLE manager_balances DROP CONSTRAINT manager_balances_pkey;
ALTER TABLE manager_balances ADD PRIMARY KEY (manager_id, currency);
