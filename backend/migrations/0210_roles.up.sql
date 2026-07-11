-- 0210_roles — Phase 3, Agent 1 (Platform & Security), range 0210–0219.
-- Contract C1-A / FR-27.1, FR-27.3: editable named roles + permission matrix,
-- replacing the Phase-2 hardcoded role→permission sets. The Phase-2 role text
-- column on managers is kept for back-compat (display/legacy fallback); real
-- authorization now resolves from role_permissions + per-manager overrides.
CREATE TABLE roles (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name        text NOT NULL UNIQUE,
    description text NOT NULL DEFAULT '',
    is_builtin  boolean NOT NULL DEFAULT false,
    -- Admin "require 2FA" can be pinned per role (FR-28.1); the global flag
    -- lives in settings (security.require_2fa).
    require_2fa boolean NOT NULL DEFAULT false,
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE role_permissions (
    role_id    uuid NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    -- A permission string (`<module>.<verb>` or a bare action perm), or the
    -- wildcard '*' meaning allow-all (the Admin role). Deny by default: the
    -- absence of a row is a denial.
    permission text NOT NULL,
    PRIMARY KEY (role_id, permission)
);

-- Flat v1 (FR-27.4): a manager holds exactly one role. Nullable so a legacy
-- row with an unrecognized role text degrades to the in-memory fallback.
ALTER TABLE managers ADD COLUMN IF NOT EXISTS role_id uuid REFERENCES roles(id);

-- Seed the three builtin roles (FR-27.3) mirroring the Phase-2 hardcoded sets
-- so the permission-engine swap is behaviour-preserving. These are editable
-- copies, not hardcoded behaviour.
INSERT INTO roles (name, description, is_builtin) VALUES
    ('admin',    'Full administrative access',              true),
    ('operator', 'Front-desk operator (Sara): subscriber view/create/edit, renew, disconnect', true),
    ('agent',    'Field agent (Hassan): scoped subscriber view, renew, reports', true);

-- Admin = allow-all via the wildcard.
INSERT INTO role_permissions (role_id, permission)
SELECT id, '*' FROM roles WHERE name = 'admin';

-- Operator = Sara's Phase-2 set.
INSERT INTO role_permissions (role_id, permission)
SELECT r.id, p
  FROM roles r
  CROSS JOIN unnest(ARRAY[
    'subscribers.view','subscribers.create','subscribers.edit',
    'profiles.view','nas.view','pools.view','live.view','sessions.view',
    'reports.view','audit.view',
    'renew','disconnect','topup','export'
  ]) AS p
 WHERE r.name = 'operator';

-- Agent = Hassan's Phase-2 set (the scoped flag on the manager limits rows).
INSERT INTO role_permissions (role_id, permission)
SELECT r.id, p
  FROM roles r
  CROSS JOIN unnest(ARRAY['subscribers.view','reports.view','renew']) AS p
 WHERE r.name = 'agent';

-- Backfill existing managers' role_id from their legacy role text.
UPDATE managers m
   SET role_id = r.id
  FROM roles r
 WHERE r.name = m.role AND m.role_id IS NULL;
