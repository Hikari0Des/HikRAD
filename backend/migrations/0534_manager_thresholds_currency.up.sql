-- v2 phase 4 / FR-69.2: same per-currency treatment as manager_balances
-- (migration 0533) for the low-balance alert thresholds (FR-20.3).

ALTER TABLE manager_low_balance_thresholds RENAME COLUMN threshold_iqd TO threshold;
ALTER TABLE manager_low_balance_thresholds ADD COLUMN currency text NOT NULL DEFAULT 'IQD' REFERENCES currencies(code);

ALTER TABLE manager_low_balance_thresholds DROP CONSTRAINT manager_low_balance_thresholds_pkey;
ALTER TABLE manager_low_balance_thresholds ADD PRIMARY KEY (manager_id, currency);
