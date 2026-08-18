-- 0601_v4_4_plan_ergonomics — v4-4 Limits engine (FR-132).
--
-- Next free number above the repo max at the time of writing (0600, this
-- same phase's seed-plans migration) — v4-5's own budget (0601-0605) has not
-- been claimed yet, so this is legitimate under the master plan's "budgets,
-- not reservations that survive being passed" rule (docs/v4/00-v4-master-plan.md
-- §6.2). A genuinely separate concern from 0597-0600 (plan ergonomics, not
-- limit enforcement), so it gets its own migration rather than being folded
-- into 0597.
--
-- auto_renew_default (profiles): the plan's default auto-renew posture for
-- subscribers placed on it. auto_renew (subscribers): the per-subscriber
-- value, owned here (sub-PRD 04, "split FR-64-style" per the phase brief's
-- own header) — the SCHEDULED TRIGGER that actually acts on it is billing's
-- (sub-PRD 05), out of this phase's scope exactly like pending_profile_id
-- is owned here and consumed by billing's renewal flow already.
ALTER TABLE profiles
    ADD COLUMN IF NOT EXISTS auto_renew_default boolean NOT NULL DEFAULT false;

ALTER TABLE subscribers
    ADD COLUMN IF NOT EXISTS auto_renew boolean NOT NULL DEFAULT false;
