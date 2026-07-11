-- 0211_manager_overrides — Phase 3, Agent 1, range 0210–0219. FR-27.1.
-- Per-manager permission overrides layered on top of the assigned role's set:
-- granted = true adds a permission the role lacks, granted = false revokes one
-- the role grants. Resolved into the effective set at login/refresh.
CREATE TABLE manager_permission_overrides (
    manager_id uuid NOT NULL REFERENCES managers(id) ON DELETE CASCADE,
    permission text NOT NULL,
    granted    boolean NOT NULL,
    PRIMARY KEY (manager_id, permission)
);
