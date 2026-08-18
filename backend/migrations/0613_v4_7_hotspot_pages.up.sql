-- v4-7 (Router manager II), FR-148. Tracks which login-page design is
-- currently pushed to a hotspot service instance's router flash, and at
-- which RouterOS version, because the page itself lives ONLY on the router
-- (RouterOS serves it straight from flash — HikRAD holds no copy the router
-- reads from at login time) and a RouterOS upgrade can wipe or relocate the
-- hotspot html directory, silently breaking self-registration/voucher login
-- until someone notices. design_version increments on every save of HikRAD's
-- own design (mode='branded_template' or 'custom'); pushed_version records
-- which version was last actually written to the router, so
-- design_version <> pushed_version (an unpushed edit) and pushed_ros_version
-- differing from the service's NAS's current RouterOS version (an upgrade
-- since the last push) are both distinct, tellable-apart staleness reasons
-- the panel surfaces rather than leaving the operator to discover a dead
-- self-registration page on their own.
--
-- One row per nas_services instance (a hotspot server has exactly one login
-- page); cascades with the service. mode='router_default' is the
-- do-nothing state (HikRAD has never pushed a page to this instance) and
-- carries no design_version/pushed_version meaning.
--
-- Forward-only (FR-51.4 precedent): no paired .down.sql.

CREATE TABLE nas_hotspot_pages (
    service_id          uuid PRIMARY KEY REFERENCES nas_services(id) ON DELETE CASCADE,
    nas_id              uuid NOT NULL REFERENCES nas(id) ON DELETE CASCADE,
    mode                text NOT NULL DEFAULT 'router_default'
        CHECK (mode IN ('router_default', 'branded_template', 'custom')),
    custom_html         text,
    design_version      int NOT NULL DEFAULT 0,
    pushed_version      int,
    pushed_at           timestamptz,
    pushed_ros_version  text,
    updated_at          timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX nas_hotspot_pages_nas_idx ON nas_hotspot_pages (nas_id);

CREATE TRIGGER nas_hotspot_pages_set_updated_at
    BEFORE UPDATE ON nas_hotspot_pages
    FOR EACH ROW EXECUTE FUNCTION radius_touch_updated_at();
