-- 0400_import_batches — Phase 5, Agent 3 (Backend Business), range 0400-0409.
-- Contract C1-D / sub-PRD 04 FR-6: the SAS4 CSV import wizard. A batch tracks
-- one uploaded file through upload -> map -> dry-run -> execute; state lives
-- here so the wizard survives a page reload between steps.

CREATE TABLE import_batches (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    filename       text NOT NULL,
    encoding       text NOT NULL DEFAULT 'utf-8',   -- utf-8 | cp1256
    delimiter      text NOT NULL DEFAULT ',',
    raw_csv        bytea NOT NULL,                   -- decoded to UTF-8 at upload time
    header         jsonb NOT NULL DEFAULT '[]',       -- source column names, in file order
    column_map     jsonb NOT NULL DEFAULT '{}',       -- {hikrad_field: source_column}
    preset         text,                              -- e.g. 'sas4', null = custom mapping
    status         text NOT NULL DEFAULT 'uploaded',  -- uploaded|mapped|dry_run|executing|completed
    row_count      int NOT NULL DEFAULT 0,
    dry_run_at     timestamptz,
    executed_at    timestamptz,
    summary        jsonb NOT NULL DEFAULT '{}',       -- {created, skipped, failed} after execute
    created_by     uuid,                              -- managers.id, nullable (actor may be deleted later)
    created_at     timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX import_batches_created_at_idx ON import_batches (created_at DESC);
