-- v2 phase 1 / FR-62 (contract C3): one NAS runs MANY service instances. v1's
-- nas.type said a router was "a PPPoE box" or "a hotspot box"; real Iraqi ISP
-- routers routinely terminate PPPoE *and* several hotspot servers (one per
-- zone/SSID) on the same box, which v1 could not express at all.
--
-- Each row is one RouterOS service instance. ros_server_name is the router's
-- own name for it (hotspot server name / PPPoE service-name); the vendor
-- adapter matches an Access-Request's identifying attributes against it to
-- decide which instance a session belongs to (C7). ip_pool_id is that
-- instance's own address pool — for hotspot this is the ONLY pool ever
-- offered, because the profile's pool is a PPPoE pool (C6 pool precedence).
--
-- Forward-only (FR-51.4): no paired .down.sql.

CREATE TABLE nas_services (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    nas_id          uuid NOT NULL REFERENCES nas(id) ON DELETE CASCADE,
    service         text NOT NULL CHECK (service IN ('pppoe', 'hotspot')),
    label           text NOT NULL DEFAULT '',
    interface_note  text NOT NULL DEFAULT '',
    ip_pool_id      uuid REFERENCES ip_pools(id) ON DELETE SET NULL,
    ros_server_name text NOT NULL DEFAULT '',
    enabled         boolean NOT NULL DEFAULT true,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX nas_services_nas_idx ON nas_services (nas_id);

CREATE TRIGGER nas_services_set_updated_at
    BEFORE UPDATE ON nas_services
    FOR EACH ROW EXECUTE FUNCTION radius_touch_updated_at();

-- Backfill: every v1 NAS becomes exactly one service instance of its old type,
-- so a v1 install's auth behaviour is bit-for-bit unchanged after upgrade (the
-- single-instance case is what C7's adapter falls back to). label names it
-- after the NAS so the panel's services sub-list is readable on day one.
INSERT INTO nas_services (nas_id, service, label, enabled)
SELECT id, type, name, enabled FROM nas;

ALTER TABLE nas DROP COLUMN type;
