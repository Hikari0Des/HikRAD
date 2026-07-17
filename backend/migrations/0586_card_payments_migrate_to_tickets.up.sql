-- v2-2 (contract C1's migration-budget table, gate item 1): backfills every
-- existing card_payments row into payment_tickets, then drops card_payments
-- — one canonical table going forward, not two overlapping ones. This is
-- the one genuinely irreversible migration in this repo's history so far
-- (no down leg exists for any migration per the repo-wide forward-only
-- rule, but this is the first one where reversing it would mean
-- reconstructing dropped data rather than un-renaming a column).
--
-- amount/currency are resolved from the PAYMENTS row linked to
-- trial_ledger_tx_id, not from ledger_transactions.amount itself:
-- card_payments' trial renewal always runs with chargeBalance=false, so its
-- ledger_transactions.amount is always 0 (no balance actually moved) — but
-- billing.renewInTx unconditionally inserts a `payments` row carrying the
-- real resolved price regardless of chargeBalance, so that row (present for
-- every card_payments record, pending/approved/rejected alike, since the
-- trial always runs at submission per FR-59.1) is the correct source. A
-- profile price/currency fallback covers the theoretical case where no
-- trial payment row exists at all.

INSERT INTO payment_tickets (
    id, subscriber_id, profile_id, method_key, provider_id,
    amount, currency, transfer_reference, transfer_date, note, method_detail,
    state, trial_ledger_tx_id, approve_ledger_tx_id, decided_by, decided_at, reject_reason,
    created_at, updated_at
)
SELECT
    cp.id, cp.subscriber_id, cp.profile_id, 'scratch_card', NULL,
    COALESCE(pay.amount, p.price), COALESCE(pay.currency, p.currency),
    NULL, NULL, '',
    jsonb_build_object('card_type', cp.card_type, 'card_code_enc', encode(cp.card_code_enc, 'base64')),
    cp.state, cp.trial_ledger_tx_id, cp.approve_ledger_tx_id, cp.decided_by, cp.decided_at, cp.reject_reason,
    cp.created_at, cp.updated_at
FROM card_payments cp
LEFT JOIN payments pay ON pay.ledger_tx_id = cp.trial_ledger_tx_id
LEFT JOIN profiles p ON p.id = cp.profile_id;

-- One synthesized 'submitted' event per migrated row, backdated to
-- created_at, so the timeline UI never shows a ticket with zero history.
INSERT INTO payment_ticket_events (ticket_id, event_type, at)
SELECT id, 'submitted', created_at FROM payment_tickets WHERE method_key = 'scratch_card';

DROP TABLE card_payments;
