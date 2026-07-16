-- v2 phase 1 / FR-64 (contract C4, amended 2026-07-16): a subscriber or profile
-- may be scoped to MANY NAS/service instances, not one.
--
-- 0502 modelled the scope as a single (nas_id, nas_service_id) pair on the row.
-- That cannot express what an operator actually has: a subscriber who should
-- authenticate on the Karrada tower AND the Mansour tower but nowhere else, or —
-- more common — on two of a router's three hotspot zones. The single pair forced
-- the choice between "one NAS" and "everywhere", so the honest answer was always
-- "everywhere", and the scope went unused.
--
-- Model: one row per allowed (nas, service) pair. nas_service_id NULL means the
-- whole NAS (every service instance on it). NO rows at all means any NAS, which
-- is v1's behaviour and stays the default — so the backfill below only has to
-- carry the scoped minority across.
--
-- Forward-only (FR-51.4): no paired .down.sql.

CREATE TABLE IF NOT EXISTS subscriber_nas_scopes (
    subscriber_id  uuid NOT NULL REFERENCES subscribers(id) ON DELETE CASCADE,
    -- ON DELETE CASCADE on the NAS, matching 0502's precedent: deleting a NAS
    -- must never delete the subscribers scoped to it. The scope row goes, which
    -- widens the account (to its remaining NASes, or to any NAS if that was its
    -- only one) — recoverable, where a cascade to the subscriber would destroy
    -- billing records.
    nas_id         uuid NOT NULL REFERENCES nas(id) ON DELETE CASCADE,
    -- ON DELETE SET NULL, NOT cascade: deleting one hotspot zone must degrade
    -- "only the Lobby zone on this NAS" to "this NAS", not to "any NAS". Keeping
    -- the nas_id half is strictly narrower than dropping the row.
    nas_service_id uuid REFERENCES nas_services(id) ON DELETE SET NULL,
    created_at     timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS profile_nas_scopes (
    profile_id     uuid NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
    nas_id         uuid NOT NULL REFERENCES nas(id) ON DELETE CASCADE,
    nas_service_id uuid REFERENCES nas_services(id) ON DELETE SET NULL,
    created_at     timestamptz NOT NULL DEFAULT now()
);

-- Deliberately NOT unique on (owner, nas_id, nas_service_id): the SET NULL above
-- can legitimately collide (two zones on one NAS, both scoped, both deleted →
-- two (nas, NULL) rows), and a unique index would turn that into a failed
-- DELETE of the service instance. Duplicates are harmless — the auth check is
-- "does ANY scope row match" — and the write path replaces the whole set and
-- dedupes before inserting.
CREATE INDEX IF NOT EXISTS subscriber_nas_scopes_subscriber_idx ON subscriber_nas_scopes (subscriber_id);
CREATE INDEX IF NOT EXISTS subscriber_nas_scopes_nas_idx ON subscriber_nas_scopes (nas_id);
CREATE INDEX IF NOT EXISTS profile_nas_scopes_profile_idx ON profile_nas_scopes (profile_id);
CREATE INDEX IF NOT EXISTS profile_nas_scopes_nas_idx ON profile_nas_scopes (nas_id);

-- Backfill: every 0502 pair becomes one scope row. Rows with a NULL nas_id were
-- "any NAS" and correctly produce no row.
INSERT INTO subscriber_nas_scopes (subscriber_id, nas_id, nas_service_id)
SELECT id, nas_id, nas_service_id FROM subscribers WHERE nas_id IS NOT NULL;

INSERT INTO profile_nas_scopes (profile_id, nas_id, nas_service_id)
SELECT id, nas_id, nas_service_id FROM profiles WHERE nas_id IS NOT NULL;

-- Drop the single-pair columns: the join table is now the only source of truth.
-- Leaving them would let the two disagree, and the AuthView loader would have to
-- pick a winner — exactly the ambiguity that made accounting read a retired
-- nas.type for a whole phase (docs/ops/known-issues.md).
DROP INDEX IF EXISTS subscribers_nas_idx;
DROP INDEX IF EXISTS subscribers_nas_service_idx;
DROP INDEX IF EXISTS profiles_nas_idx;
DROP INDEX IF EXISTS profiles_nas_service_idx;

ALTER TABLE subscribers DROP COLUMN IF EXISTS nas_id, DROP COLUMN IF EXISTS nas_service_id;
ALTER TABLE profiles DROP COLUMN IF EXISTS nas_id, DROP COLUMN IF EXISTS nas_service_id;
