-- 0101_profiles_columns — Phase 2, Agent 4 (Backend Business), range 0100–0109.
-- Contract C1-D / sub-PRD 04 §4 / FR-8, FR-9, FR-10, FR-58. Adds the plan
-- governance fields (quota, expiry/quota behaviors, pool, session default,
-- Hotspot rate) on top of the Phase-1 skeleton (0002). Additive only.

ALTER TABLE profiles
    -- Active-service IP pool (FR-8): Framed-Pool at auth. No DB foreign key —
    -- B's ip_pools table is migration 0120, which runs AFTER this one, so the
    -- reference is validated in the API (pool must exist) rather than the schema.
    ADD COLUMN IF NOT EXISTS pool_id               uuid,
    -- Default simultaneous-session limit (FR-5.1); a per-subscriber override wins.
    ADD COLUMN IF NOT EXISTS session_limit_default int NOT NULL DEFAULT 1,
    -- Quota model (FR-8): unlimited | total bytes | split down/up bytes.
    ADD COLUMN IF NOT EXISTS quota_mode            text NOT NULL DEFAULT 'unlimited'
                              CHECK (quota_mode IN ('unlimited', 'total', 'split')),
    ADD COLUMN IF NOT EXISTS quota_total_bytes     bigint,
    ADD COLUMN IF NOT EXISTS quota_down_bytes      bigint,
    ADD COLUMN IF NOT EXISTS quota_up_bytes        bigint,
    -- Throttle target rate (abstract "rx/tx" string) for quota_behavior=throttle.
    ADD COLUMN IF NOT EXISTS throttle_rate         text,
    -- FR-9: hard block vs. walled-garden expired pool at auth.
    ADD COLUMN IF NOT EXISTS expiry_behavior       text NOT NULL DEFAULT 'block'
                              CHECK (expiry_behavior IN ('block', 'expired_pool')),
    -- FR-10: block | throttle | expired_pool on quota exhaustion.
    ADD COLUMN IF NOT EXISTS quota_behavior        text NOT NULL DEFAULT 'block'
                              CHECK (quota_behavior IN ('block', 'throttle', 'expired_pool')),
    -- Archive-not-delete (FR-8): a profile in use is archived, never removed.
    ADD COLUMN IF NOT EXISTS archived              boolean NOT NULL DEFAULT false,
    -- FR-58.1: Hotspot-specific rate (kbps). NULL = fall back to the main rate.
    ADD COLUMN IF NOT EXISTS hotspot_rate_down_kbps int,
    ADD COLUMN IF NOT EXISTS hotspot_rate_up_kbps   int,
    ADD COLUMN IF NOT EXISTS updated_at            timestamptz NOT NULL DEFAULT now();

CREATE INDEX IF NOT EXISTS profiles_archived_idx ON profiles (archived);

-- Reuse the business_touch_updated_at function created in 0100.
DROP TRIGGER IF EXISTS profiles_set_updated_at ON profiles;
CREATE TRIGGER profiles_set_updated_at
    BEFORE UPDATE ON profiles
    FOR EACH ROW EXECUTE FUNCTION business_touch_updated_at();
