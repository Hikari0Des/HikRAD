-- 0203_profile_burst_tod — Phase 3, Agent 4, range 0200–0209.
-- Contract C1-D / C4 / FR-11. Adds the burst-rate and time-of-day rule schema B
-- consumes: burst fields flow into D's AuthView (populated in subscribers) and
-- are rendered to VSAs only by B's vendor adapter (FR-17); TOD windows are read
-- by B's boundary sweeps through the injected TODProvider seam (radius/tod.go).
-- Additive only; every column defaults so existing profiles migrate untouched.

-- Burst segments (FR-11). Abstract "rx/tx" intents — no vendor syntax here. They
-- apply only to the normal/full-speed reply (radius/policy.go composeRate).
ALTER TABLE profiles
    ADD COLUMN IF NOT EXISTS burst_rate      text,   -- e.g. '20M/20M'
    ADD COLUMN IF NOT EXISTS burst_threshold text,   -- e.g. '15M/15M'
    ADD COLUMN IF NOT EXISTS burst_time      text,   -- e.g. '16/16' (seconds)
    ADD COLUMN IF NOT EXISTS rate_priority   text,   -- e.g. '8'
    ADD COLUMN IF NOT EXISTS min_rate        text;   -- e.g. '1M/1M'

-- Time-of-day windows (FR-11). A profile may define one or more windows granting
-- a speed boost and/or a quota exemption (e.g. free night speed 00:00–06:00).
-- start_min/end_min are minutes since local (Asia/Baghdad) midnight; end < start
-- wraps past midnight. boost_rate/normal_rate are abstract rx/tx intents; empty
-- boost_rate = exemption-only window. Column names match radius.TODWindow.
CREATE TABLE profile_tod_windows (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    profile_id  uuid NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
    label       text NOT NULL DEFAULT '',
    start_min   int  NOT NULL CHECK (start_min BETWEEN 0 AND 1439),
    end_min     int  NOT NULL CHECK (end_min   BETWEEN 0 AND 1439),
    boost_rate  text NOT NULL DEFAULT '',
    normal_rate text NOT NULL DEFAULT '',
    exempt      boolean NOT NULL DEFAULT false,
    enabled     boolean NOT NULL DEFAULT true,
    created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX profile_tod_profile_idx ON profile_tod_windows (profile_id) WHERE enabled;
