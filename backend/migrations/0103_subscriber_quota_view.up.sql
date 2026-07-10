-- 0103_subscriber_quota_view — Phase 2, Agent 4, range 0100–0109.
-- Contract C1-D: the read-only view C's quota evaluator (internal/accounting)
-- consumes to know each subscriber's quota model + cycle anchor. Column names
-- are frozen by C's reader (quota.go): subscriber_id, quota_mode,
-- quota_total_bytes, quota_down_bytes, quota_up_bytes, cycle_anchor.
--
-- Overrides note: quota is a profile-level concept (FR-8); there is no
-- per-subscriber quota override this phase, so the view reads straight from the
-- joined profile. A subscriber with no profile reads as 'unlimited'.

CREATE OR REPLACE VIEW subscriber_quota_view AS
SELECT
    s.id                              AS subscriber_id,
    COALESCE(p.quota_mode, 'unlimited') AS quota_mode,
    p.quota_total_bytes               AS quota_total_bytes,
    p.quota_down_bytes                AS quota_down_bytes,
    p.quota_up_bytes                  AS quota_up_bytes,
    s.quota_cycle_anchor              AS cycle_anchor
FROM subscribers s
LEFT JOIN profiles p ON p.id = s.profile_id;
