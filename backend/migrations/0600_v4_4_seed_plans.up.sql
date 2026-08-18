-- 0600_v4_4_seed_plans — v4-4 Limits engine (FR-132).
--
-- Seeds a starter plan set so a fresh install is usable before the operator
-- has configured anything (sub-PRD 04 / gate item 10). Guarded by
-- NOT EXISTS (SELECT 1 FROM profiles): an install that already has even one
-- profile — including an upgraded existing install — is left completely
-- untouched. This is a one-shot seed, not a reconciled default set: it never
-- re-runs (migrations apply once) and never touches a row it didn't insert.
--
-- Three speed tiers plus the "Expired" throttle plan the brief calls for: a
-- slow, always-available plan meant as a switch_plan/expired-pool target
-- (FR-130) rather than something ever sold — Price 0, a decade-long
-- duration so it is never itself the thing that expires.
INSERT INTO profiles (
    name, price, currency, duration_days, rate_down_kbps, rate_up_kbps,
    session_limit_default, quota_mode, expiry_behavior, quota_behavior
)
SELECT v.name, v.price, 'IQD', v.duration_days, v.rate_down_kbps, v.rate_up_kbps,
       v.session_limit_default, 'unlimited', 'block', 'block'
  FROM (VALUES
        ('Basic 5M',      15000::bigint, 30::int, 5120::int,  5120::int, 1::int),
        ('Standard 10M',  25000::bigint, 30::int, 10240::int, 10240::int, 2::int),
        ('Premium 20M',   40000::bigint, 30::int, 20480::int, 20480::int, 3::int),
        ('Expired',           0::bigint, 3650::int, 512::int,   512::int, 1::int)
       ) AS v(name, price, duration_days, rate_down_kbps, rate_up_kbps, session_limit_default)
 WHERE NOT EXISTS (SELECT 1 FROM profiles);
