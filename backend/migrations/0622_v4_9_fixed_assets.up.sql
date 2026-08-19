-- v4-9 (FR-157): fixed assets with straight-line depreciation. Cash/bank/
-- capital accounts need no new table — they are just chart-of-accounts rows
-- (migration 0616's subtype='cash'/'bank'/'capital'), which is why this
-- migration is fixed_assets only.

CREATE TABLE fixed_assets (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name              text NOT NULL,
    cost              bigint NOT NULL CHECK (cost > 0),
    acquired_date     date NOT NULL DEFAULT CURRENT_DATE,
    useful_life_months int NOT NULL CHECK (useful_life_months > 0),
    salvage_value     bigint NOT NULL DEFAULT 0 CHECK (salvage_value >= 0),
    disposed_at       timestamptz,
    acquisition_journal_entry_id uuid REFERENCES journal_entries(id),
    created_by        uuid REFERENCES managers(id),
    created_at        timestamptz NOT NULL DEFAULT now()
);

-- One row per (asset, month) actually run — straight-line
-- (cost - salvage) / useful_life_months, capped so total depreciation never
-- exceeds cost - salvage. The UNIQUE constraint is what makes "run this
-- month's depreciation" idempotent (a re-run skips months already posted).
CREATE TABLE depreciation_entries (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    fixed_asset_id   uuid NOT NULL REFERENCES fixed_assets(id),
    period           date NOT NULL, -- first-of-month
    amount           bigint NOT NULL CHECK (amount > 0),
    journal_entry_id uuid REFERENCES journal_entries(id),
    created_at       timestamptz NOT NULL DEFAULT now(),
    UNIQUE (fixed_asset_id, period)
);
CREATE INDEX depreciation_entries_asset_idx ON depreciation_entries (fixed_asset_id, period);
