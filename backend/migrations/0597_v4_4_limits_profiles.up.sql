-- 0597_v4_4_limits_profiles — v4-4 Limits engine (FR-128, FR-129, FR-130).
-- Next free number above the repo max (0596) — the linear-sequence rule is
-- absolute (CLAUDE.md).
--
-- Adds the two new limits (uptime, daily) and widens the unified exhaustion
-- action set (block | throttle | switch_plan | expired_pool) onto ALL FOUR
-- limits — expiry and quota included. expiry_behavior/quota_behavior (0101)
-- are REUSED as the action column for their limit rather than duplicated:
-- their existing values (block/expired_pool and block/throttle/expired_pool)
-- are an exact subset of the new set, so every existing row and every
-- pre-v4-4 radius test keeps deciding exactly as it did (gate item 12).
--
-- Each limit gets one exhaust_target text column alongside its action —
-- meaning depends on the action: a rate string for throttle, a target
-- profile id for switch_plan, an optional pool-name override for
-- expired_pool (empty = fall back to the global purpose='expired' pool, the
-- existing v1 convention). One flexible column per limit, per the phase
-- brief's "an exhaust_action + exhaust_target pair — four pairs, not one
-- shared" (docs/v4/phases/phase-v4-4-limits-engine/00-phase.md §3.1).

ALTER TABLE profiles
    -- FR-128: cumulative connected-time ceiling. NULL/0 = no uptime limit.
    ADD COLUMN IF NOT EXISTS uptime_limit_seconds  bigint,
    -- FR-129: per-day byte cap + the time-of-day it resets, in the server
    -- timezone (Asia/Baghdad — matches the existing profile_tod_windows /
    -- radius.startTODSweeps convention). NULL/0 = no daily limit.
    ADD COLUMN IF NOT EXISTS daily_limit_bytes      bigint,
    ADD COLUMN IF NOT EXISTS daily_reset_min        int NOT NULL DEFAULT 0
                              CHECK (daily_reset_min BETWEEN 0 AND 1439),

    -- Expiry's action reuses expiry_behavior (widened below); its own target
    -- column is new (expiry never had a throttle rate or switch target before).
    ADD COLUMN IF NOT EXISTS expiry_exhaust_target  text NOT NULL DEFAULT '',
    -- Quota's action reuses quota_behavior (widened below); throttle already
    -- had throttle_rate (0101) — exhaust_target is additionally used only by
    -- the new switch_plan action (throttle_rate stays quota's throttle target
    -- so existing rows/tests reading it are untouched).
    ADD COLUMN IF NOT EXISTS quota_exhaust_target   text NOT NULL DEFAULT '',

    -- FR-128 uptime: a genuinely new limit, so both action + target are new.
    ADD COLUMN IF NOT EXISTS uptime_exhaust_action  text NOT NULL DEFAULT 'block',
    ADD COLUMN IF NOT EXISTS uptime_exhaust_target  text NOT NULL DEFAULT '',

    -- FR-129 daily: default action is THROTTLE, not block — the commercial
    -- distinction the phase brief calls out (a daily cap silently defaulting
    -- to a hard disconnect would be the wrong product default). Self-restores
    -- at the next daily_reset_min with no operator action (radius/limits.go).
    ADD COLUMN IF NOT EXISTS daily_exhaust_action   text NOT NULL DEFAULT 'throttle',
    ADD COLUMN IF NOT EXISTS daily_exhaust_target   text NOT NULL DEFAULT '';

-- Widen the existing expiry_behavior/quota_behavior CHECK constraints
-- additively (FR-130). Constraint names are looked up rather than assumed —
-- 0101 added them as inline column CHECKs, which Postgres auto-names, and
-- guessing wrong would either no-op (leaving the old, narrower check in
-- place and breaking switch_plan/throttle-for-expiry) or fail outright.
DO $$
DECLARE r record;
BEGIN
    FOR r IN
        SELECT conname FROM pg_constraint
         WHERE conrelid = 'profiles'::regclass
           AND contype = 'c'
           AND pg_get_constraintdef(oid) ILIKE '%expiry_behavior%'
    LOOP
        EXECUTE format('ALTER TABLE profiles DROP CONSTRAINT %I', r.conname);
    END LOOP;
END $$;
ALTER TABLE profiles ADD CONSTRAINT profiles_expiry_behavior_check
    CHECK (expiry_behavior IN ('block', 'expired_pool', 'throttle', 'switch_plan'));

DO $$
DECLARE r record;
BEGIN
    FOR r IN
        SELECT conname FROM pg_constraint
         WHERE conrelid = 'profiles'::regclass
           AND contype = 'c'
           AND pg_get_constraintdef(oid) ILIKE '%quota_behavior%'
    LOOP
        EXECUTE format('ALTER TABLE profiles DROP CONSTRAINT %I', r.conname);
    END LOOP;
END $$;
ALTER TABLE profiles ADD CONSTRAINT profiles_quota_behavior_check
    CHECK (quota_behavior IN ('block', 'throttle', 'expired_pool', 'switch_plan'));

ALTER TABLE profiles ADD CONSTRAINT profiles_uptime_exhaust_action_check
    CHECK (uptime_exhaust_action IN ('block', 'throttle', 'expired_pool', 'switch_plan'));
ALTER TABLE profiles ADD CONSTRAINT profiles_daily_exhaust_action_check
    CHECK (daily_exhaust_action IN ('block', 'throttle', 'expired_pool', 'switch_plan'));
