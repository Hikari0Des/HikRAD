-- v2 phase 1 / FR-61 (contract C2): a subscriber's service is a three-valued
-- type, not a PPPoE account with a hotspot opt-in bit. 'hotspot' is the new
-- capability — a full subscriber record (quota, expiry, session limit all
-- apply) that has no PPPoE access at all.
--
-- Backfill maps the v1 bit exactly: allow_hotspot=false was PPPoE-only, and
-- allow_hotspot=true was FR-58 dual-service (PPPoE *plus* hotspot). No v1 row
-- can be 'hotspot'-only, because v1 had no way to express it — which is the
-- gap this phase closes.
--
-- Forward-only (FR-51.4): no paired .down.sql. Dropping back to the bit would
-- collapse 'hotspot' and 'dual' onto true, silently granting hotspot-only
-- accounts PPPoE on a re-upgrade. See the phase doc's migration-range note.

ALTER TABLE subscribers
    ADD COLUMN IF NOT EXISTS service_type text NOT NULL DEFAULT 'pppoe'
        CHECK (service_type IN ('pppoe', 'hotspot', 'dual'));

UPDATE subscribers SET service_type = CASE WHEN allow_hotspot THEN 'dual' ELSE 'pppoe' END;

ALTER TABLE subscribers DROP COLUMN allow_hotspot;

-- The unified subscriber list (C10) filters by service_type over the same
-- keyset-paginated query the list already uses, which orders by id. Index the
-- filter alongside that keyset key — service_type alone is too low-cardinality
-- to be worth an index, but (service_type, id) lets a filtered page seek
-- straight to its cursor instead of sorting a filtered heap scan.
CREATE INDEX IF NOT EXISTS subscribers_service_type_idx ON subscribers (service_type, id);
