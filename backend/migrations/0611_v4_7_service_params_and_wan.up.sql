-- v4-7 (Router manager II), FR-145 (service tabs) + FR-149 (IPv6 dual-stack).
--
-- Part 1 — nas_services gains the extra per-instance parameters the FR-145
-- service-tab forms expose beyond what v2 phase 1/3 already stored
-- (ros_server_name/ip_pool_id/management_mode): an optional per-service
-- accounting interim-interval override, the hotspot user-profile name a
-- system-managed hotspot server provisions (distinct from the router's
-- global "default" profile — FR-62.7's health check already warned about
-- that one being the wrong lever), a DHCP range for a HikRAD-created hotspot
-- pool, and the FR-149 IPv6 opt-in fields (delegated-prefix length, and the
-- pool name the wizard/service form constructs for DHCPv6-PD). All nullable
-- or zero-valued so every pre-v4-7 row (backfilled 'router'-mode services)
-- reads as "no override, IPv6 off" without a data migration.
--
-- Part 2 — nas_wan_uplinks: the FR-145 Internet tab's own object type.
-- Mirrors nas_services.management_mode exactly (Decision 48's router-
-- ownership principle applied to a new object class): 'system' rows are
-- HikRAD-created WAN interfaces (editable, pushable); 'router' rows are
-- uplinks HikRAD discovered already configured (shown read-only, per the
-- phase brief's "ours editable, pre-existing shown read-only"). One row per
-- (nas_id, interface) — a router only has one WAN config per physical/
-- logical interface at a time.
--
-- Part 3 — subscribers.ipv6_static_prefix (FR-149's per-subscriber static
-- prefix pin for business accounts). Added here, from the RADIUS/NAS module,
-- to the subscribers table owned by sub-PRD 04 — the same cross-domain
-- column-addition precedent FR-64 already established for
-- subscribers.nas_id/nas_service_id. NULL = no pin, the subscriber receives
-- a prefix from the resolved service's IPv6 pool at login time as normal.
--
-- Forward-only (FR-51.4 precedent): no paired .down.sql.

ALTER TABLE nas_services
    ADD COLUMN interim_interval_secs int,
    ADD COLUMN hotspot_user_profile  text NOT NULL DEFAULT '',
    ADD COLUMN dhcp_range            text NOT NULL DEFAULT '',
    ADD COLUMN ipv6_enabled          boolean NOT NULL DEFAULT false,
    ADD COLUMN ipv6_prefix_len       int NOT NULL DEFAULT 56
        CHECK (ipv6_prefix_len BETWEEN 48 AND 64),
    ADD COLUMN ipv6_pool_name        text NOT NULL DEFAULT '';

CREATE TABLE nas_wan_uplinks (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    nas_id          uuid NOT NULL REFERENCES nas(id) ON DELETE CASCADE,
    interface       text NOT NULL,
    label           text NOT NULL DEFAULT '',
    mode            text NOT NULL CHECK (mode IN ('static', 'dhcp', 'pppoe')),
    address         text NOT NULL DEFAULT '',
    gateway         text NOT NULL DEFAULT '',
    management_mode text NOT NULL DEFAULT 'router'
        CHECK (management_mode IN ('router', 'system')),
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    UNIQUE (nas_id, interface)
);

CREATE INDEX nas_wan_uplinks_nas_idx ON nas_wan_uplinks (nas_id);

CREATE TRIGGER nas_wan_uplinks_set_updated_at
    BEFORE UPDATE ON nas_wan_uplinks
    FOR EACH ROW EXECUTE FUNCTION radius_touch_updated_at();

ALTER TABLE subscribers
    ADD COLUMN ipv6_static_prefix text;
