-- v2 phase 4: the Phase-4 e-wallet gateway intents table gets the same
-- treatment for consistency (no `_iqd`-suffixed column should survive this
-- phase anywhere in the schema) even though this subsystem has no live
-- gateway behind it (Decision 29) and is slated for retirement by v2-2's
-- FR-E. Renaming it now avoids a stray inconsistent column name in the
-- meantime, and costs one line.

ALTER TABLE payment_intents RENAME COLUMN amount_iqd TO amount;
ALTER TABLE payment_intents ADD COLUMN currency text NOT NULL DEFAULT 'IQD' REFERENCES currencies(code);
