-- v3-5 (FR-108/FR-109). Next free number above the repo max (0592) — the
-- linear-sequence rule is absolute (docs/ops/known-issues.md, 2026-07-17).

-- FR-109: a named MikroTik address-list a NORMAL hotspot/PPPoE login places the
-- subscriber's IP into (today an address-list is only emitted for the expired
-- redirect). Configured per-profile, with an optional per-subscriber override,
-- following the exact nullable-text pattern of profiles.throttle_rate /
-- subscribers.rate_override ("NULL = inherit / emit nothing").
ALTER TABLE profiles ADD COLUMN IF NOT EXISTS address_list text;
ALTER TABLE subscribers ADD COLUMN IF NOT EXISTS address_list_override text;

-- FR-108: IP-pool ownership (router-ownership sync principle, PRD Decision 48,
-- generalizing FR-67's nas_services.management_mode to pools).
--   'system' = HikRAD-owned: full edit/delete, and may be pushed to a router's
--              /ip pool. This is the DEFAULT so every existing HikRAD-defined
--              named pool stays fully editable/deletable exactly as today
--              (defaulting to 'router' would silently make every current pool
--              read-only — a behavior regression).
--   'router' = adopted/imported from a router: a read-only reference HikRAD
--              never writes back. Only pools created via the (new) adopt path
--              are inserted with this mode.
ALTER TABLE ip_pools ADD COLUMN IF NOT EXISTS management_mode text NOT NULL DEFAULT 'system'
  CHECK (management_mode IN ('router', 'system'));

-- router_ranges records, verbatim, what the router reported for an ADOPTED pool.
-- It is deliberately text, not inet[]: RouterOS expresses pool ranges as
-- "10.0.0.2-10.0.0.254" start-end strings, which are not valid inet values, and
-- converting them would be lossy guesswork about a pool HikRAD does not own.
-- An adopted pool therefore carries ranges = '{}' (HikRAD models no ranges of
-- its own for it) plus this observed string for display. NULL on system pools,
-- whose authoritative ranges live in ip_pools.ranges as before.
ALTER TABLE ip_pools ADD COLUMN IF NOT EXISTS router_ranges text;
