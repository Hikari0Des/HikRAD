-- v2 phase 4 / FR-69.1: profile price gains a currency; a renewal now charges
-- in the profile's own currency end to end. Owned by sub-PRD 04 (FR-8) but
-- migrated here, in the one session reworking the ledger currency model, per
-- the phase brief's rationale (docs/v2/phases/phase-v2-4-multi-currency/00-phase.md).

ALTER TABLE profiles RENAME COLUMN price_iqd TO price;
ALTER TABLE profiles ADD COLUMN currency text NOT NULL DEFAULT 'IQD' REFERENCES currencies(code);
