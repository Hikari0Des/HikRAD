-- 0200_ledger — Phase 3, Agent 4 (Backend Business), range 0200–0209.
-- Contract C1-D / sub-PRD 05 FR-24 (immutable ledger) + FR-20 (manager balances
-- derived from it). This is the money core: every balance is DERIVED as the sum
-- of a manager's ledger entries — never a stored-and-edited field.

-- The append-only transaction ledger (FR-24). amount_iqd is the SIGNED effect on
-- actor_manager_id's balance: a renewal debits (negative), a top-up/refund
-- credits (positive). So balance(M) = sum(amount_iqd where actor_manager_id = M)
-- exactly (FR-20.1 / AC-20b). Revenue is tracked separately via payments so
-- voucher/agent balance mechanics never distort it.
CREATE TABLE ledger_transactions (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    at               timestamptz NOT NULL DEFAULT now(),
    type             text NOT NULL CHECK (type IN
                        ('renewal','topup','manual_payment','voucher_redeem','refund','adjustment','discount')),
    amount_iqd       bigint NOT NULL,               -- signed balance effect on actor_manager_id
    -- actor_manager_id is the manager whose BALANCE this entry moves (the agent
    -- who renewed, or the agent being topped up). The manager who physically
    -- performed the action is recorded in audit_log, not here.
    actor_manager_id uuid REFERENCES managers(id) ON DELETE SET NULL,
    subscriber_id    uuid REFERENCES subscribers(id) ON DELETE SET NULL,
    source           text NOT NULL DEFAULT 'panel',  -- panel|agent|voucher|portal-<gw>
    reference        text NOT NULL DEFAULT '',        -- receipt no / voucher id / gateway ref
    -- reverses_id links a refund/correction to the entry it reverses (FR-25);
    -- the unique index enforces "refund of an already-refunded tx rejected".
    reverses_id      uuid REFERENCES ledger_transactions(id),
    note             text NOT NULL DEFAULT ''
);
CREATE INDEX ledger_actor_idx      ON ledger_transactions (actor_manager_id, at DESC);
CREATE INDEX ledger_subscriber_idx ON ledger_transactions (subscriber_id, at DESC);
CREATE INDEX ledger_type_idx       ON ledger_transactions (type, at DESC);
CREATE INDEX ledger_at_idx         ON ledger_transactions (at DESC);
CREATE UNIQUE INDEX ledger_reverses_uniq
    ON ledger_transactions (reverses_id) WHERE reverses_id IS NOT NULL;

-- Immutability (AC-24a), mirroring audit_log (0112). The application connects as
-- the table owner, against which GRANT/REVOKE is a no-op, so a BEFORE trigger is
-- what actually enforces "UPDATE/DELETE refused at DB level"; the REVOKE is
-- defense-in-depth for any future least-privilege application role.
CREATE OR REPLACE FUNCTION ledger_immutable() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'ledger_transactions is append-only: % is not permitted', TG_OP
        USING ERRCODE = 'restrict_violation';
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER ledger_no_update
    BEFORE UPDATE ON ledger_transactions
    FOR EACH ROW EXECUTE FUNCTION ledger_immutable();

CREATE TRIGGER ledger_no_delete
    BEFORE DELETE ON ledger_transactions
    FOR EACH ROW EXECUTE FUNCTION ledger_immutable();

REVOKE UPDATE, DELETE ON ledger_transactions FROM PUBLIC;

-- Cached balance materialization (FR-20.1). balance_iqd is ALWAYS recomputed as
-- the exact sum of the manager's ledger entries inside the renewal/topup/refund
-- txn (SELECT ... FOR UPDATE serializes concurrent movements per manager), so
-- cache ≡ ledger holds by construction — the property test asserts it. This is a
-- read cache for display, never an authoritative store.
CREATE TABLE manager_balances (
    manager_id  uuid PRIMARY KEY REFERENCES managers(id) ON DELETE CASCADE,
    balance_iqd bigint NOT NULL DEFAULT 0,
    updated_at  timestamptz NOT NULL DEFAULT now()
);

-- Low-balance thresholds (FR-20.3) read by C's agent_balance_low alert rule.
CREATE TABLE manager_low_balance_thresholds (
    manager_id    uuid PRIMARY KEY REFERENCES managers(id) ON DELETE CASCADE,
    threshold_iqd bigint NOT NULL DEFAULT 0
);
