-- 0204_renewal_idempotency — Phase 3, Agent 4, range 0200–0209.
-- Contract C2 edge case: double-submit renewal (idempotency key header honored).
-- A renewal carrying an Idempotency-Key reserves the key as the FIRST write in
-- its transaction; a concurrent duplicate blocks on the unique PK, then reads the
-- committed response — so exactly one charge occurs and both callers see the same
-- {ledger_tx_id, receipt_no, ...} result.
CREATE TABLE renewal_idempotency (
    idem_key      text PRIMARY KEY,
    subscriber_id uuid,
    response      jsonb,
    at            timestamptz NOT NULL DEFAULT now()
);
