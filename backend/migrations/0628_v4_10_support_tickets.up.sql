-- 0628 — v4-10 (Support tickets), FR-164. Named support_tickets, deliberately
-- distinct from the existing payment_tickets table (0583, a payment-
-- confirmation workflow, not customer support) so nobody ever confuses the
-- two by name. Scoped by the existing auth.ScopeFilter via subscribers'
-- owner_manager_id, the exact same join billing/tickets_api.go's
-- ticketFilters already uses for payment_tickets (an agent sees only their
-- own subscribers' tickets — reused, not reimplemented).
CREATE TABLE support_tickets (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    -- Human-facing ticket number (e.g. "TCK-000123"), assigned from
    -- support_ticket_seq below — stable, sequential, never reused.
    number           text NOT NULL UNIQUE,
    subscriber_id    uuid NOT NULL REFERENCES subscribers(id) ON DELETE CASCADE,
    subject          text NOT NULL,
    priority         text NOT NULL DEFAULT 'normal'
                       CHECK (priority IN ('normal','moderate','critical')),
    status           text NOT NULL DEFAULT 'open'
                       CHECK (status IN ('open','in_progress','resolved','closed','withdrawn')),
    -- Who opened it: an admin/agent on the customer's behalf, the subscriber
    -- themself (portal, v4-11), or the Telegram bot (this phase).
    created_by_type  text NOT NULL CHECK (created_by_type IN ('manager','subscriber','bot')),
    created_by_id    uuid,
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now(),
    resolved_at      timestamptz,
    closed_at        timestamptz
);

CREATE SEQUENCE support_ticket_seq;

CREATE INDEX support_tickets_subscriber_idx ON support_tickets (subscriber_id);
CREATE INDEX support_tickets_status_idx ON support_tickets (status, created_at DESC);
CREATE INDEX support_tickets_priority_idx ON support_tickets (priority);
-- Search by number/subject (FR-164's queue filter chips + search).
CREATE INDEX support_tickets_number_idx ON support_tickets (number);

CREATE TRIGGER support_tickets_set_updated_at
    BEFORE UPDATE ON support_tickets
    FOR EACH ROW EXECUTE FUNCTION notify_touch_updated_at();
