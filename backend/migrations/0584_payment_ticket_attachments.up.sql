-- v2-2 / FR-78.2 (contract C10): attachment metadata for a payment ticket.
-- Files themselves live on local disk under the data directory (NFR-7:
-- never a database blob, never remote object storage) — this row is the
-- pointer + retrieval metadata. content_type is validated against the
-- file's real content at upload time (Go layer, not this migration) so a
-- forced Content-Disposition at retrieval can never be undermined by a
-- client-supplied MIME type lie.

CREATE TABLE payment_ticket_attachments (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    ticket_id    uuid NOT NULL REFERENCES payment_tickets(id) ON DELETE CASCADE,
    filename     text NOT NULL,
    stored_path  text NOT NULL,
    content_type text NOT NULL,
    size_bytes   bigint NOT NULL,
    uploaded_at  timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX payment_ticket_attachments_ticket_idx ON payment_ticket_attachments (ticket_id);
