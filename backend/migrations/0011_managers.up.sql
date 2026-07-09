-- 0011_managers — contract C6 (Phase 1, Agent A). Panel manager accounts;
-- roles/permissions expand in later phases (sub-PRD 06), do not pre-add.
CREATE TABLE managers (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    username      text NOT NULL UNIQUE,
    password_hash text NOT NULL,
    role          text NOT NULL DEFAULT 'admin',
    created_at    timestamptz NOT NULL DEFAULT now()
);
