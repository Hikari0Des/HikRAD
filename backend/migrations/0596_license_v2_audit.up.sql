-- 0596_license_v2_audit — Phase v4-3 (FR-124). Append-only audit of every
-- license state-machine TRANSITION (not every periodic evaluation — a
-- no-op tick every 10 minutes forever would carry zero information and grow
-- unboundedly), each row carrying the fingerprint the server had live AT
-- THAT MOMENT — so "why did we land in grace on 2026-08-20" is answerable
-- from history, not just the current row's state.
CREATE TABLE license_state_audit (
    id                   bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    key_id               text NOT NULL,
    from_state           text NOT NULL CHECK (from_state IN ('valid', 'grace', 'expired_grace')),
    to_state             text NOT NULL CHECK (to_state IN ('valid', 'grace', 'expired_grace')),
    issued_fingerprint   text NOT NULL,
    current_fingerprint  text NOT NULL,
    reason               text NOT NULL,
    occurred_at          timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX license_state_audit_occurred_at_idx ON license_state_audit (occurred_at);

-- Immutability, same discipline as audit_log (migration 0112) and the money
-- ledger: the application connects as the table owner, against which
-- GRANT/REVOKE is a no-op, so a BEFORE trigger is what actually enforces
-- "UPDATE/DELETE refused at DB level" — including against a direct psql
-- edit, which is exactly the forgery vector this phase's gate item 7 tests.
-- The REVOKE is defense-in-depth for any future least-privilege application
-- role.
CREATE OR REPLACE FUNCTION license_state_audit_immutable() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'license_state_audit is append-only: % is not permitted', TG_OP
        USING ERRCODE = 'restrict_violation';
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER license_state_audit_no_update
    BEFORE UPDATE ON license_state_audit
    FOR EACH ROW EXECUTE FUNCTION license_state_audit_immutable();

CREATE TRIGGER license_state_audit_no_delete
    BEFORE DELETE ON license_state_audit
    FOR EACH ROW EXECUTE FUNCTION license_state_audit_immutable();

REVOKE UPDATE, DELETE ON license_state_audit FROM PUBLIC;
