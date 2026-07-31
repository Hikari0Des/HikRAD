-- 0594_v3_6_import_entity — Phase v3-6 (FR-111.3), sub-PRD 04. Makes the shared
-- CSV-import batch tables entity-aware so the same upload → map → dry-run →
-- execute wizard can load a PROFILE catalog, not only subscribers.
--
-- The default backfills every pre-existing batch as 'subscribers' — the only
-- entity that has ever existed — so the subscriber import path is byte-for-byte
-- unchanged: an existing install upgrades with all historical batches correctly
-- labelled and no code branch behaving differently for them.
ALTER TABLE import_batches
    ADD COLUMN IF NOT EXISTS entity text NOT NULL DEFAULT 'subscribers'
    CHECK (entity IN ('subscribers', 'profiles'));
