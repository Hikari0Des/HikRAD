-- 0122_pool_assignments — Phase 2, Agent 2 (RADIUS & NAS), range 0120–0129.
-- Contract C1-B / FR-16: links a pool to the profiles or NAS devices that use
-- it. The primary profile→pool reference is D's profiles.pool_id column; this
-- table carries the many-to-many NAS↔pool relation and any additional
-- profile↔pool links the wizard creates.
CREATE TABLE pool_assignments (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    pool_id    uuid NOT NULL REFERENCES ip_pools(id) ON DELETE CASCADE,
    -- Exactly one target: a profile or a NAS. No FK to profiles here so the
    -- two agents' migration ranges stay independent; the API validates the
    -- referenced profile exists via D's read model.
    profile_id uuid,
    nas_id     uuid REFERENCES nas(id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT pool_assignment_one_target CHECK (
        (profile_id IS NOT NULL)::int + (nas_id IS NOT NULL)::int = 1
    )
);

-- A pool is assigned to a given profile / NAS at most once.
CREATE UNIQUE INDEX pool_assignments_profile_idx
    ON pool_assignments (pool_id, profile_id) WHERE profile_id IS NOT NULL;
CREATE UNIQUE INDEX pool_assignments_nas_idx
    ON pool_assignments (pool_id, nas_id) WHERE nas_id IS NOT NULL;
