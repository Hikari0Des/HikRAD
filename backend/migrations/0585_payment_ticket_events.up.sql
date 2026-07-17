-- v2-2 / FR-79.1 (contract C8): the ticket timeline. Every state-changing
-- operation (submit, attachment write, approve, reject) inserts exactly one
-- row here in the SAME transaction as the state change it records, so the
-- timeline can never drift from the state it describes — nothing writes
-- payment_tickets state without also writing the event that explains it.
-- This is also the sole source FR-80's notification layer reads from
-- (never a second, parallel invented message).

CREATE TABLE payment_ticket_events (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    ticket_id         uuid NOT NULL REFERENCES payment_tickets(id) ON DELETE CASCADE,
    event_type        text NOT NULL, -- submitted | attachment_added | attachment_failed | trial_granted | approved | rejected
    actor_manager_id  uuid,
    note              text,
    at                timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX payment_ticket_events_ticket_idx ON payment_ticket_events (ticket_id, at);
