-- 0603_v4_5_card_groups — v4-5 Subscribers, counters & card groups (FR-136,
-- Decision 65). "Card groups replace flat vouchers": a batch of prepaid
-- hotspot-resale credentials carrying their OWN speed/transfer/uptime/expiry
-- limits, independent of any subscriber profile — the SAMM model.
--
-- A card group is deliberately NOT a profile: profiles govern SUBSCRIBERS
-- (long-lived accounts renewed over time); a card group governs a BATCH of
-- disposable, printed, single-owner-until-exhausted credentials with their
-- own shelf date. Reusing profiles would mean every printed-card price plan
-- also shows up in every subscriber-facing plan picker, which is wrong for
-- both screens.
CREATE TABLE card_groups (
    id                   uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name                 text NOT NULL,
    description          text NOT NULL DEFAULT '',
    username_prefix      text NOT NULL DEFAULT '',
    code_format          text NOT NULL DEFAULT 'alphanumeric'
                           CHECK (code_format IN ('numeric', 'alpha', 'alphanumeric')),
    code_length          int  NOT NULL DEFAULT 10 CHECK (code_length BETWEEN 6 AND 24),
    -- price is IQD (v3-2 single-book, FR-101.3 — plans/cards are IQD-only).
    price                bigint NOT NULL DEFAULT 0 CHECK (price >= 0),
    rate_down_kbps       int NOT NULL DEFAULT 0 CHECK (rate_down_kbps >= 0),
    rate_up_kbps         int NOT NULL DEFAULT 0 CHECK (rate_up_kbps >= 0),
    -- Transfer limit: unlimited | combined (one ceiling over down+up) |
    -- separate (independent down/up ceilings) — the three modes the phase
    -- brief names explicitly.
    transfer_mode        text NOT NULL DEFAULT 'unlimited'
                           CHECK (transfer_mode IN ('unlimited', 'combined', 'separate')),
    transfer_total_bytes bigint,          -- combined mode; NULL otherwise
    transfer_down_bytes  bigint,          -- separate mode; NULL otherwise
    transfer_up_bytes    bigint,          -- separate mode; NULL otherwise
    uptime_limit_seconds bigint,          -- NULL = unlimited
    -- expire_after_seconds counts from FIRST LOGIN (cards.first_login_at),
    -- not from creation — a card sitting unused on a shelf never expires on
    -- this clock; only valid_until (below) can end it before first use.
    expire_after_seconds bigint,
    -- valid_until is a HARD SHELF DATE, checked INDEPENDENTLY of the
    -- post-login expiry window (gate item 7): a card inside its post-login
    -- window but past valid_until is still refused.
    valid_until          timestamptz,
    enabled              boolean NOT NULL DEFAULT true,
    -- source distinguishes a group an operator created here from the
    -- synthetic ones migration 0605 creates from pre-existing voucher
    -- batches, purely for the panel to label them and route redemption of a
    -- legacy code through the untouched voucher path (see 0605's own header).
    source               text NOT NULL DEFAULT 'native' CHECK (source IN ('native', 'legacy_voucher')),
    creator_manager_id   uuid REFERENCES managers(id) ON DELETE SET NULL,
    created_at           timestamptz NOT NULL DEFAULT now(),
    updated_at           timestamptz NOT NULL DEFAULT now()
);

CREATE TRIGGER card_groups_set_updated_at
    BEFORE UPDATE ON card_groups
    FOR EACH ROW EXECUTE FUNCTION business_touch_updated_at();
