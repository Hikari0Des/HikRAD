-- 0212_ip_allowlist — Phase 3, Agent 1, range 0210–0219. FR-30.
-- Optional per-manager CIDR allowlist. An empty list (no rows) means no
-- restriction; a non-empty list is enforced at login and on every request
-- (the effective list is embedded in the access token and re-resolved on
-- refresh, so a change takes effect within one access-token lifetime).
CREATE TABLE manager_ip_allowlist (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    manager_id uuid NOT NULL REFERENCES managers(id) ON DELETE CASCADE,
    cidr       cidr NOT NULL,
    note       text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (manager_id, cidr)
);

CREATE INDEX manager_ip_allowlist_mgr_idx ON manager_ip_allowlist (manager_id);
