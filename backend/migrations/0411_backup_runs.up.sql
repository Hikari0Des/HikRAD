-- 0411_backup_runs — Phase 5, Agent 1, range 0410-0419. Contract C1-A / C5 / FR-51.
-- One row per backup attempt (nightly job or `hikrad backup now`), giving
-- `hikrad backup list` and the Settings > System "last backup age" indicator
-- something to read without shelling out to `ls` on the backups directory.
CREATE TABLE backup_runs (
    id             bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    filename       text NOT NULL,
    started_at     timestamptz NOT NULL,
    finished_at    timestamptz,
    size_bytes     bigint,
    schema_version bigint,
    encrypted      boolean NOT NULL DEFAULT true,
    status         text NOT NULL DEFAULT 'running' CHECK (status IN ('running', 'ok', 'failed')),
    error          text,
    trigger        text NOT NULL DEFAULT 'manual' CHECK (trigger IN ('manual', 'scheduled', 'pre_update')),
    created_at     timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX backup_runs_started_idx ON backup_runs (started_at DESC);
