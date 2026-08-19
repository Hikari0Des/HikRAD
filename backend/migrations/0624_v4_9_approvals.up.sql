-- v4-9 (FR-159): the agent approval gate, plus per-agent commission
-- percentage and credit limit (threaded into the existing v2-9 wholesale/
-- retail resolution by commission_credit.go, not replacing it).
--
-- ONE generic table for every approvable kind (undo, invoice void, expense
-- void, subscriber delete) rather than one table per kind — the point of
-- FR-159's "approval replays the exact same code path" contract is that
-- approving never runs a bespoke second implementation; a generic
-- kind+payload row backed by internal/approvals' registry is what makes
-- that structurally true rather than a matter of discipline.

ALTER TABLE managers
    ADD COLUMN IF NOT EXISTS commission_pct numeric(5,2) NOT NULL DEFAULT 0
        CHECK (commission_pct >= 0 AND commission_pct <= 100),
    ADD COLUMN IF NOT EXISTS credit_limit bigint NOT NULL DEFAULT 0
        CHECK (credit_limit >= 0);

-- Widens the ledger's type domain the same way v2-4 added 'exchange'
-- (migration 0532) — a legitimate new entry type going forward, not a
-- reinterpretation of any existing row (§3.1/§4: the existing ledger is
-- never rewritten in this phase).
ALTER TABLE ledger_transactions DROP CONSTRAINT ledger_transactions_type_check;
ALTER TABLE ledger_transactions ADD CONSTRAINT ledger_transactions_type_check
    CHECK (type IN ('renewal','topup','manual_payment','voucher_redeem','refund','adjustment','discount','exchange','commission'));

CREATE TABLE approval_requests (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    kind           text NOT NULL,
    payload        jsonb NOT NULL,
    requested_by   uuid NOT NULL REFERENCES managers(id),
    description    text NOT NULL DEFAULT '',
    state          text NOT NULL DEFAULT 'pending' CHECK (state IN ('pending', 'approved', 'rejected', 'withdrawn')),
    decided_by     uuid REFERENCES managers(id),
    decided_at     timestamptz,
    reject_reason  text,
    result         jsonb,
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX approval_requests_state_idx     ON approval_requests (state, created_at DESC);
CREATE INDEX approval_requests_requester_idx ON approval_requests (requested_by, created_at DESC);
