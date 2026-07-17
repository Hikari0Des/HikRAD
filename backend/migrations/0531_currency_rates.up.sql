-- v2 phase 4 / FR-68.3-68.4 (contract C5): admin-maintained exchange rates —
-- the ONLY source of exchange rates in HikRAD (NFR-7: no online rate feed).
-- rate is expressed in WHOLE-currency terms (e.g. "1 USD = 1310 IQD" stores
-- rate=1310, from_currency='USD', to_currency='IQD') so the admin-facing
-- rate-entry form stays human-auditable; conversion code applies each side's
-- currencies.minor_unit_digits around this whole-currency rate.
--
-- Append-only from the API's perspective (FR-68.4): a new rate is always a
-- new row with effective_from=now(), never an UPDATE of an old one, so a
-- currency_rate_id stamped on a historical ledger row (migration 0532) is
-- permanently the rate that was actually used — no DB-level enforcement is
-- needed here because nothing in this schema ever needs to edit a rate row;
-- the application code simply never issues an UPDATE against this table.
--
-- Created before 0532 (ledger currency) so ledger_transactions.currency_rate_id
-- has a table to reference.

CREATE TABLE currency_rates (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    from_currency  text NOT NULL REFERENCES currencies(code),
    to_currency    text NOT NULL REFERENCES currencies(code),
    rate           numeric(20,8) NOT NULL,
    effective_from timestamptz NOT NULL DEFAULT now(),
    created_by     uuid REFERENCES managers(id),
    created_at     timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX currency_rates_pair_idx ON currency_rates (from_currency, to_currency, effective_from DESC);
