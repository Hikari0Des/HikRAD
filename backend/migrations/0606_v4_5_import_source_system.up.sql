-- 0606_v4_5_import_source_system — v4-5 FR-138: legacy importers (Radius
-- Manager / Super Speed Radius MySQL dumps, Pro Radius XLSX) share the
-- existing entity-parameterised FR-6/FR-111.3 wizard rather than becoming a
-- fourth import path. The one genuinely new piece of state the wizard needs
-- is WHICH legacy system a batch came from, for the "post-import prompt to
-- review imported plan prices" and general traceability the brief calls for
-- — `preset` already names a specific default column-map guess, but an
-- operator using a custom mapping (no preset) for one of these systems would
-- otherwise leave no record of which legacy product it came from.
ALTER TABLE import_batches
    ADD COLUMN IF NOT EXISTS source_system text NOT NULL DEFAULT 'custom'
        CHECK (source_system IN ('custom', 'sas4', 'radius_manager', 'super_speed_radius', 'pro_radius'));
