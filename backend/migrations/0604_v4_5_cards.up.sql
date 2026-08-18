-- 0604_v4_5_cards — v4-5 card groups (FR-136). One row per printed/generated
-- credential. Codes are stored hashed-only (sha256, normalized — identical
-- scheme to billing/voucher.go's, deliberately: migration 0605 copies
-- vouchers.code_hash into cards.code_hash VERBATIM, byte-for-byte, which is
-- only possible because both tables hash the same normalized plaintext the
-- same way).
--
-- state is a persisted, operator-visible summary (unused/active/exhausted/
-- disabled) kept fresh by the RADIUS card-auth path re-deriving and writing
-- it on every authorization attempt (radius/cardauth.go) — it is NEVER the
-- source of truth for an accept/reject decision (that is always evaluateLimits
-- run fresh against live usage, exactly like a subscriber), only a display
-- cache so the cards list doesn't need to re-run that derivation per row.
--
-- Per-card counters are DERIVED live from sessions/usage_points filtered by
-- the new card_id column below (added to BOTH tables, alongside the existing
-- nullable subscriber_id) — the same "derive, never store-and-edit" posture
-- subscriber_counters (0598) and subscriber_billing_counters (0602) already
-- follow. usage_cycle_anchor is the one piece of resettable state: "reset an
-- individual card's counters" (group action, FR-136) is exactly "move this
-- anchor forward," identical in spirit to subscriber_counters' anchors.
CREATE TABLE cards (
    id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    group_id           uuid NOT NULL REFERENCES card_groups(id) ON DELETE CASCADE,
    code_hash          text NOT NULL UNIQUE,
    state              text NOT NULL DEFAULT 'unused'
                          CHECK (state IN ('unused', 'active', 'exhausted', 'disabled')),
    first_login_at     timestamptz,
    usage_cycle_anchor timestamptz NOT NULL DEFAULT now(),
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX cards_group_idx ON cards (group_id, state);

CREATE TRIGGER cards_set_updated_at
    BEFORE UPDATE ON cards
    FOR EACH ROW EXECUTE FUNCTION business_touch_updated_at();

-- card_id is nullable and carries NO foreign key, exactly mirroring
-- subscriber_id's own posture on both tables (0130/0131) — accounting must
-- keep recording a session/usage-point even for a card the registry doesn't
-- yet know about (orphan tolerance), and a hard FK would risk losing a
-- packet over referential integrity, which M2 never allows.
ALTER TABLE sessions      ADD COLUMN card_id uuid;
ALTER TABLE usage_points  ADD COLUMN card_id uuid;

CREATE INDEX sessions_card_idx      ON sessions (card_id, started_at DESC) WHERE card_id IS NOT NULL;
CREATE INDEX usage_points_card_idx  ON usage_points (card_id, time DESC)  WHERE card_id IS NOT NULL;
