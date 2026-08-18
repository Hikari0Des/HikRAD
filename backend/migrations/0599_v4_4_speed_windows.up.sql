-- 0599_v4_4_speed_windows — v4-4 Limits engine (FR-131).
--
-- Speed windows are a NEW, separate mechanism from the existing FR-11
-- profile_tod_windows (0203) — deliberately not a replacement. The old TOD
-- windows stay exactly as they are (radius/tod.go, its tests, and every
-- pre-v4-4 test are untouched, gate item 12); speed windows add the two
-- things TOD windows don't have: day-of-week control and "highest speed
-- wins" resolution among several overlapping matches, and are excluded for a
-- subscriber currently throttled by an exhaustion action (radius/policy.go,
-- radius/limits.go) — a rule that does not apply to the old TOD windows.
--
-- days_mask: bit N set = day N is included, bit0=Sunday .. bit6=Saturday
-- (matches Go's time.Weekday numbering so radius/limits.go needs no
-- remapping table). 0 = every day (empty set reads as "unrestricted", the
-- same convention profile/subscriber NAS scopes already use — an empty set
-- is never "nowhere").
--
-- rate_down_kbps/rate_up_kbps (not a pre-composed rate string): matches
-- profiles.rate_down_kbps/rate_up_kbps (0002) so the loader renders the
-- abstract "rx/tx" intent through the exact same rateString() helper
-- subscribers/authview.go already uses for the profile's base rate — one
-- rendering path, not two.
CREATE TABLE profile_speed_windows (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    profile_id     uuid NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
    label          text NOT NULL DEFAULT '',
    days_mask      int  NOT NULL DEFAULT 0 CHECK (days_mask BETWEEN 0 AND 127),
    start_min      int  NOT NULL CHECK (start_min BETWEEN 0 AND 1439),
    end_min        int  NOT NULL CHECK (end_min BETWEEN 0 AND 1439),
    rate_down_kbps int  NOT NULL CHECK (rate_down_kbps > 0),
    rate_up_kbps   int  NOT NULL CHECK (rate_up_kbps > 0),
    enabled        boolean NOT NULL DEFAULT true,
    created_at     timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX profile_speed_windows_profile_idx ON profile_speed_windows (profile_id) WHERE enabled;
