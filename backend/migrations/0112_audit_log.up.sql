-- 0112_audit_log — Phase 2, Agent 1, range 0110–0119. Contract C1-A / FR-28.3.
-- Append-only record of every mutating manager action: actor, action,
-- entity, before/after JSON diff, IP, user-agent, timestamp. Same
-- immutability discipline as the money ledger (sub-PRD 05 FR-24).
CREATE TABLE audit_log (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    actor_id    uuid,                       -- null for system/anonymous events (e.g. failed login for unknown user)
    action      text NOT NULL,              -- e.g. 'subscribers.update', 'auth.login', 'auth.denied'
    entity_type text NOT NULL DEFAULT '',
    entity_id   text NOT NULL DEFAULT '',
    before      jsonb,
    after       jsonb,
    ip          text NOT NULL DEFAULT '',
    ua          text NOT NULL DEFAULT '',
    at          timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX audit_log_entity_idx ON audit_log (entity_type, entity_id, at DESC);
CREATE INDEX audit_log_actor_idx  ON audit_log (actor_id, at DESC);

-- Immutability (AC-28c). The application connects as the table owner, against
-- which GRANT/REVOKE is a no-op, so a BEFORE trigger is what actually enforces
-- "UPDATE/DELETE refused at DB level". The REVOKE is defense-in-depth for any
-- future least-privilege application role.
CREATE OR REPLACE FUNCTION audit_log_immutable() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'audit_log is append-only: % is not permitted', TG_OP
        USING ERRCODE = 'restrict_violation';
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER audit_log_no_update
    BEFORE UPDATE ON audit_log
    FOR EACH ROW EXECUTE FUNCTION audit_log_immutable();

CREATE TRIGGER audit_log_no_delete
    BEFORE DELETE ON audit_log
    FOR EACH ROW EXECUTE FUNCTION audit_log_immutable();

REVOKE UPDATE, DELETE ON audit_log FROM PUBLIC;
