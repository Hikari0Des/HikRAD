-- 0401_import_rows — Phase 5, Agent 3, range 0400-0409. One row per CSV data
-- line. dry-run populates errors/warnings/action without writing to
-- subscribers; execute consumes action='create' rows and idempotently marks
-- them imported (re-running skips a row already status='imported').

CREATE TABLE import_rows (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    batch_id      uuid NOT NULL REFERENCES import_batches(id) ON DELETE CASCADE,
    row_number    int NOT NULL,          -- 1-based, header excluded
    fields        jsonb NOT NULL,        -- mapped {hikrad_field: value} for this row
    errors        jsonb NOT NULL DEFAULT '[]',
    warnings      jsonb NOT NULL DEFAULT '[]',
    action        text NOT NULL DEFAULT 'skip',   -- create|skip
    status        text NOT NULL DEFAULT 'pending', -- pending|imported|skipped|failed
    subscriber_id uuid,                             -- set once execute creates the subscriber
    error         text,                             -- execute-time failure, if any
    UNIQUE (batch_id, row_number)
);
CREATE INDEX import_rows_batch_idx ON import_rows (batch_id, row_number);
