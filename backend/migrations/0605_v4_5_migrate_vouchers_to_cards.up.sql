-- 0605_v4_5_migrate_vouchers_to_cards — v4-5 FR-136 / Decision 65: "Existing
-- vouchers migrate into synthetic groups... no voucher code changes."
--
-- This migration is ADDITIVE ONLY: it INSERTs a card_groups/cards row for
-- every existing voucher_batches/vouchers row, and touches NOTHING else —
-- zero UPDATE, zero DELETE against voucher_batches or vouchers. The original
-- redemption path (billing/voucher.go, POST /api/v1/vouchers/redeem) is
-- completely unmodified and keeps working exactly as it did before this
-- phase, for exactly the reason gate item 8 demands ("every pre-existing
-- code still redeems"): the safest way to guarantee that is to never touch
-- the rows the guarantee is about. The panel's unified Cards list renders
-- source='legacy_voucher' groups alongside native ones for a single
-- browsing experience, but redeeming one of these still dispatches to the
-- untouched voucher endpoint under the hood (cards/api.go).
--
-- code_hash is copied VERBATIM — not re-hashed, not re-derived from any
-- plaintext (none exists; only the hash was ever stored) — because
-- cards.code_hash uses the IDENTICAL sha256(normalized-uppercase) scheme
-- billing/voucher.go already established. A verbatim column copy is what
-- makes "byte-for-byte" trivially true rather than merely intended.
--
-- Primary keys are reused 1:1 (card_groups.id = voucher_batches.id,
-- cards.id = vouchers.id) rather than adding a separate linking column —
-- the correspondence is then exact and needs no extra lookup.
INSERT INTO card_groups
    (id, name, description, username_prefix, code_format, code_length, price,
     rate_down_kbps, rate_up_kbps, transfer_mode, valid_until, enabled, source,
     creator_manager_id, created_at)
SELECT
    b.id,
    'Legacy voucher batch ' || COALESCE(NULLIF(b.prefix, ''), left(b.id::text, 8)),
    'Migrated from voucher_batches (Decision 65). Redemption still runs through the original voucher path — this is a display-unification projection, not a new credential.',
    b.prefix,
    'alphanumeric',
    10, -- vouchers never recorded a code length; FR-22.1's own floor
    b.unit_price,
    COALESCE(p.rate_down_kbps, 0),
    COALESCE(p.rate_up_kbps, 0),
    'unlimited', -- a voucher funded a renewal; it never carried its own transfer cap
    b.expires_at, -- code-expiry maps directly onto the shelf-date concept
    (b.state = 'active'),
    'legacy_voucher',
    b.creator_manager_id,
    b.created_at
FROM voucher_batches b
LEFT JOIN profiles p ON p.id = b.profile_id;

INSERT INTO cards (id, group_id, code_hash, state, first_login_at, created_at, updated_at)
SELECT
    v.id,
    v.batch_id,
    v.code_hash,
    CASE
        WHEN v.state = 'used' THEN 'exhausted' -- spent: a redeemed voucher can never be used again
        WHEN v.state = 'void' THEN 'disabled'
        ELSE 'unused'
    END,
    v.used_at,
    b.created_at,
    COALESCE(v.used_at, b.created_at)
FROM vouchers v
JOIN voucher_batches b ON b.id = v.batch_id;
