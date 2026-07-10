-- 0100_subscribers_columns — Phase 2, Agent 4 (Backend Business), range 0100–0109.
-- Contract C1-D / sub-PRD 04 §4 / FR-1, FR-5, FR-7, FR-58, FR-55. Adds the
-- operator-facing subscriber fields on top of the Phase-1 skeleton (0001).
-- Additive only; every column is nullable or defaulted so existing rows (the
-- seeded testuser) migrate without a data backfill.

ALTER TABLE subscribers
    ADD COLUMN IF NOT EXISTS address                text,
    ADD COLUMN IF NOT EXISTS notes                  text,
    -- Flat v1 ownership (FR-27.4): a single manager reference, no reseller tree.
    -- SET NULL on manager delete so a subscriber is never orphaned into an
    -- unresolvable owner (it falls back to unscoped/admin visibility).
    ADD COLUMN IF NOT EXISTS owner_manager_id       uuid REFERENCES managers(id) ON DELETE SET NULL,
    -- MAC lock (FR-5.2): off | learn-first | fixed. learned_mac holds the
    -- Calling-Station-Id captured on first auth in learn mode (B's authorize
    -- path calls radius LearnMac → subscribers.LearnMac writes it here).
    ADD COLUMN IF NOT EXISTS mac_lock_mode          text NOT NULL DEFAULT 'off'
                              CHECK (mac_lock_mode IN ('off', 'learn', 'fixed')),
    ADD COLUMN IF NOT EXISTS learned_mac            text,
    -- Optional fixed Framed-IP-Address (FR-16.2), validated against B's
    -- static-purpose pools + a per-subscriber uniqueness rule before save.
    ADD COLUMN IF NOT EXISTS static_ip              inet,
    -- Per-user overrides (FR-7). NULL = inherit the profile. rate_override is an
    -- abstract MikroTik-Rate-Limit string ("rx/tx", e.g. "2M/10M"); price is IQD.
    ADD COLUMN IF NOT EXISTS session_limit_override int,
    ADD COLUMN IF NOT EXISTS rate_override          text,
    ADD COLUMN IF NOT EXISTS price_override         bigint,
    -- Reason captured when an operator disables an account (FR-1.2); retained
    -- even if the account later expires so lists keep showing why.
    ADD COLUMN IF NOT EXISTS disabled_reason        text,
    -- FR-58.1: this PPPoE subscriber may also authenticate on Hotspot NASes.
    ADD COLUMN IF NOT EXISTS allow_hotspot          boolean NOT NULL DEFAULT false,
    -- FR-55 / FR-1.5: consent to Phase-4 subscriber messaging. Requires a valid
    -- phone to enable (enforced in the API, not the schema).
    ADD COLUMN IF NOT EXISTS whatsapp_opt_in        boolean NOT NULL DEFAULT false,
    -- FR-12 (Could): profile change scheduled for next renewal. The column
    -- exists so Phase-3 renewals can consume it; no scheduling logic this phase.
    ADD COLUMN IF NOT EXISTS pending_profile_id     uuid REFERENCES profiles(id) ON DELETE SET NULL,
    -- Billing-cycle start for quota accounting (FR-8/FR-10). C's quota evaluator
    -- sums usage_points since this instant. Set at creation and (Phase 3) reset
    -- on renewal. Defaults to now() so existing rows get a sane cycle.
    ADD COLUMN IF NOT EXISTS quota_cycle_anchor     timestamptz NOT NULL DEFAULT now(),
    ADD COLUMN IF NOT EXISTS updated_at             timestamptz NOT NULL DEFAULT now();

-- profile_id predates this migration (0001) without a foreign key; adopt one now
-- so archive-not-delete (FR-8) is enforced even against a direct SQL delete.
ALTER TABLE subscribers
    DROP CONSTRAINT IF EXISTS subscribers_profile_id_fkey,
    ADD CONSTRAINT subscribers_profile_id_fkey
        FOREIGN KEY (profile_id) REFERENCES profiles(id) ON DELETE RESTRICT;

CREATE INDEX IF NOT EXISTS subscribers_owner_idx   ON subscribers (owner_manager_id);
CREATE INDEX IF NOT EXISTS subscribers_profile_idx ON subscribers (profile_id);
-- Lists and the expiry sweep both filter on status + expiry.
CREATE INDEX IF NOT EXISTS subscribers_status_idx  ON subscribers (status);
CREATE INDEX IF NOT EXISTS subscribers_expires_idx ON subscribers (expires_at);

-- Namespaced touch-updated_at (business_* range) so it cannot collide with the
-- radius_touch_updated_at function another agent defined in migration 0120.
CREATE OR REPLACE FUNCTION business_touch_updated_at() RETURNS trigger AS $$
BEGIN
    NEW.updated_at := now();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS subscribers_set_updated_at ON subscribers;
CREATE TRIGGER subscribers_set_updated_at
    BEFORE UPDATE ON subscribers
    FOR EACH ROW EXECUTE FUNCTION business_touch_updated_at();
