-- 0629 — v4-10 (Support tickets), FR-164. support_ticket_messages: the
-- thread on a support_tickets row. `internal` is the data-level distinction
-- the phase's most severe binding rule hangs on — an internal staff note
-- must never reach a subscriber-facing response, on every endpoint, proven
-- by test rather than by a UI filter. Append-only (same discipline as
-- audit_log/journal_entries): a message, once posted, is never edited or
-- deleted — correcting a mistake means posting a new message, so the
-- thread a subscriber already saw can never be silently rewritten out from
-- under them.
CREATE TABLE support_ticket_messages (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    ticket_id   uuid NOT NULL REFERENCES support_tickets(id) ON DELETE CASCADE,
    author_type text NOT NULL CHECK (author_type IN ('manager','subscriber','system','bot')),
    author_id   uuid,
    body        text NOT NULL,
    -- true = staff-only note, false = a reply visible to the subscriber.
    -- Never nullable, never inferred from author_type (a manager can post
    -- either kind) — always an explicit choice made at write time.
    internal    boolean NOT NULL DEFAULT false,
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX support_ticket_messages_ticket_idx ON support_ticket_messages (ticket_id, created_at);

CREATE TRIGGER support_ticket_messages_no_update BEFORE UPDATE ON support_ticket_messages FOR EACH ROW EXECUTE FUNCTION notify_immutable();
CREATE TRIGGER support_ticket_messages_no_delete BEFORE DELETE ON support_ticket_messages FOR EACH ROW EXECUTE FUNCTION notify_immutable();
REVOKE UPDATE, DELETE ON support_ticket_messages FROM PUBLIC;
