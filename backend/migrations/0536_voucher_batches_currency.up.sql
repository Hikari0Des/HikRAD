-- v2 phase 4 / FR-69.4: voucher batches keep the FR-22 charge-at-generation
-- model exactly as Phase 3 built it — this only adds that unit_price/currency
-- are the generating profile's. Every pre-migration batch's profile was
-- IQD-priced, so the default is exact, not an approximation.

ALTER TABLE voucher_batches RENAME COLUMN unit_price_iqd TO unit_price;
ALTER TABLE voucher_batches ADD COLUMN currency text NOT NULL DEFAULT 'IQD' REFERENCES currencies(code);
