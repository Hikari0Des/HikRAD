-- v2 phase 9 / FR-72 (contract C2): margin on the ledger. Every renewal
-- ledger row can stamp the plan cost that was in force at the moment of
-- sale, resolved from profile_cost_history (migration 0540) at renewal time
-- — nullable, since a plan with no recorded cost yet must report margin as
-- UNKNOWN, never silently render as zero cost / 100% profit.
--
-- No currency column of its own: cost_at_sale is always expressed in the
-- ledger row's own existing `currency` column (migration 0532). A plan whose
-- cost is recorded in a different currency than it sells in is a display-only
-- concern for reports (never converted onto this column — see the phase
-- brief C1/C9).
--
-- Margin (amount - cost_at_sale) is NEVER stored — only ever computed on
-- read, the same append-only "derive, don't store-and-edit" discipline the
-- ledger already applies to balances (FR-20.1).

ALTER TABLE ledger_transactions ADD COLUMN cost_at_sale bigint;
