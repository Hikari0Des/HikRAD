-- 0201_payments — Phase 3, Agent 4, range 0200–0209.
-- Contract C1-D / sub-PRD 05 FR-21 (receipts) + FR-24. A payment is the
-- customer-facing GROSS billing record behind a renewal/manual-cash/voucher/
-- e-wallet charge, carrying the sequential receipt number. Revenue is derived
-- from here (see revenue_daily) so balance/voucher mechanics never distort it.

-- Per-install sequential receipt number source (FR-21). The settings prefix is
-- prepended in code; the sequence guarantees monotonic uniqueness.
CREATE SEQUENCE IF NOT EXISTS receipt_seq START 1;

CREATE TABLE payments (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    receipt_no    text NOT NULL UNIQUE,
    ledger_tx_id  uuid NOT NULL REFERENCES ledger_transactions(id),
    subscriber_id uuid REFERENCES subscribers(id) ON DELETE SET NULL,
    amount_iqd    bigint NOT NULL,          -- gross customer-billed price (signed: refunds negative)
    method        text NOT NULL,            -- renewal|cash|voucher|refund|portal-<gw>
    source        text NOT NULL DEFAULT 'panel',
    -- share_token backs the shareable/print-only receipt link (FR-21) that needs
    -- no auth; it is an unguessable random token, never the receipt_no.
    share_token   text UNIQUE,
    at            timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX payments_at_idx         ON payments (at DESC);
CREATE INDEX payments_subscriber_idx ON payments (subscriber_id, at DESC);

-- A receipt is an immutable record; a reprint must never mutate it (FR-21). The
-- INSERT-only discipline is enforced in code, but the trigger makes it a DB
-- guarantee too (same pattern as ledger/audit).
CREATE OR REPLACE FUNCTION payments_immutable() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'payments is append-only: % is not permitted', TG_OP
        USING ERRCODE = 'restrict_violation';
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER payments_no_update
    BEFORE UPDATE ON payments
    FOR EACH ROW EXECUTE FUNCTION payments_immutable();

CREATE TRIGGER payments_no_delete
    BEFORE DELETE ON payments
    FOR EACH ROW EXECUTE FUNCTION payments_immutable();

REVOKE UPDATE, DELETE ON payments FROM PUBLIC;

-- Frozen read-only revenue view (C1-D → C5 dashboard revenue tile / FR-32 and
-- reports FR-45). Gross customer-billed money by local (Asia/Baghdad) day and
-- source; refunds appear as negative payment rows so the view nets correctly.
CREATE VIEW revenue_daily AS
    SELECT (at AT TIME ZONE 'Asia/Baghdad')::date AS date,
           source,
           sum(amount_iqd)::bigint             AS amount
      FROM payments
     GROUP BY 1, 2;
