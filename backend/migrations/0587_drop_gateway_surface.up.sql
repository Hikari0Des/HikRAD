-- v2-2 (contract C12, kickoff blocker 2): FR-23 (e-wallet gateways) is
-- retired entirely, not quarantined — there is no remaining consumer once
-- FR-77-80 ship (Decision 37). Drops both gateway-era tables:
--
--   payment_intents  — a transient pending->confirmed->renewed record that
--                      was always superseded by the real money movement in
--                      ledger_transactions/payments once a gateway callback
--                      landed; those tables are untouched and outside this
--                      migration's scope. No backfill needed — a pending or
--                      failed intent was never a record of money that
--                      actually moved.
--   gateway_configs  — per-gateway enable/mode/creds; no adapter exists to
--                      configure once this ships, so the settings are dead.
--
-- No FK from any other table references either (verified against every
-- migration file before writing this one). A real gateway integration, if
-- ever pursued, is a fresh brief with real credentials, not a resurrection
-- of this schema.

DROP TABLE payment_intents;
DROP TABLE gateway_configs;
