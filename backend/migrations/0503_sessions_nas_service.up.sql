-- v2 phase 1 / FR-62, migration 0503 (the range the phase doc reserved for
-- "follow-ups discovered during build" — this is one).
--
-- Why: the accounting pipeline derived a session's service from nas.type
-- (internal/accounting/resolve.go), which 0501 removed. A session must still
-- know its service — FR-58.2's per-service session counting and every
-- service-filtered report depend on it — and now that a NAS runs many
-- instances, it should record WHICH instance it belongs to, not just the kind.
-- That is also what makes the panel's per-service live count (FR-63) and the
-- gate's "live sessions carry the service instance" leg possible.
--
-- sessions.service (pppoe|hotspot) stays as-is: it is the coarse kind, still
-- correct, still what reports group by. nas_service_id is additive and
-- nullable, because a session on an unregistered NAS (orphan tolerance, the
-- zeroUUID sentinel) genuinely has no instance to point at.
--
-- ON DELETE SET NULL: deleting a service instance must not delete the session
-- history that ran through it — that history is billing evidence.
--
-- Forward-only (FR-51.4): no paired .down.sql.

ALTER TABLE sessions
    ADD COLUMN IF NOT EXISTS nas_service_id uuid REFERENCES nas_services(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS sessions_nas_service_idx
    ON sessions (nas_service_id) WHERE nas_service_id IS NOT NULL;
