-- v2-6 / FR-84.1 (contract C1): per-manager preferences, presentation-only.
-- Absence of a row is the valid "no preferences set yet" state — no row is
-- created at manager-creation time and this migration backfills nothing for
-- existing managers, same "query, don't mirror" posture as v2-9's
-- profile_cost_history. PUT upserts, so the first write creates the row.

CREATE TABLE manager_preferences (
    manager_id     uuid PRIMARY KEY REFERENCES managers(id) ON DELETE CASCADE,
    schema_version int NOT NULL DEFAULT 1,
    doc            jsonb NOT NULL DEFAULT '{}'::jsonb,
    updated_at     timestamptz NOT NULL DEFAULT now()
);
