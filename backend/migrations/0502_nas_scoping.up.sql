-- v2 phase 1 / FR-64 (contract C4): scope a subscriber or a profile to a NAS
-- and/or a specific service instance, enforced at RADIUS auth (nas_not_allowed).
-- v1 had no way to say "this account only exists on the Karrada tower" — any
-- valid credential authenticated on any registered NAS.
--
-- Both columns nullable; the nullable pair means "any NAS", which is v1's
-- behaviour and therefore the correct default for every existing row (no
-- backfill: NULL is already right).
--
-- ON DELETE SET NULL, not CASCADE: deleting a NAS must never delete the
-- subscribers scoped to it — it widens their scope back to "any NAS", which is
-- recoverable, where a cascade would silently destroy billing records.
--
-- Forward-only (FR-51.4): no paired .down.sql.

ALTER TABLE subscribers
    ADD COLUMN IF NOT EXISTS nas_id uuid REFERENCES nas(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS nas_service_id uuid REFERENCES nas_services(id) ON DELETE SET NULL;

ALTER TABLE profiles
    ADD COLUMN IF NOT EXISTS nas_id uuid REFERENCES nas(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS nas_service_id uuid REFERENCES nas_services(id) ON DELETE SET NULL;

-- Partial indexes: the overwhelming majority of rows are NULL (any-NAS), so
-- index only the scoped minority. These serve the panel's "which subscribers
-- are pinned to this NAS?" lookup and the ON DELETE SET NULL fan-out.
CREATE INDEX IF NOT EXISTS subscribers_nas_idx ON subscribers (nas_id) WHERE nas_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS subscribers_nas_service_idx ON subscribers (nas_service_id) WHERE nas_service_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS profiles_nas_idx ON profiles (nas_id) WHERE nas_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS profiles_nas_service_idx ON profiles (nas_service_id) WHERE nas_service_id IS NOT NULL;
