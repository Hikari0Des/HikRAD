-- 0233_monitored_devices — Phase 3, Agent 3. Contract C1-C / C5 / FR-60
-- (amendment 2026-07-11). Infrastructure devices the probe engine watches
-- ALONGSIDE the NAS registry — APs, switches, non-RADIUS routers, servers.
-- Deliberately a SEPARATE table from `nas`: devices are never FreeRADIUS
-- clients, never appear in NAS lists/wizard, and carry no RADIUS secret. They
-- share only the probe engine and the probe-history API shape.
CREATE TABLE monitored_devices (
    id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name               text NOT NULL,
    ip                 inet NOT NULL UNIQUE,
    type               text NOT NULL DEFAULT 'other'
                         CHECK (type IN ('ap', 'switch', 'router', 'server', 'other')),
    -- Optional SNMP v2c community, AES-GCM sealed at rest via platform/crypto
    -- (same envelope as nas.snmp_community_enc, NFR-4). Null → ICMP only.
    snmp_community_enc bytea,
    location           text NOT NULL DEFAULT '',
    notes              text NOT NULL DEFAULT '',
    enabled            boolean NOT NULL DEFAULT true,
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX monitored_devices_enabled_idx ON monitored_devices (enabled);

CREATE TRIGGER monitored_devices_set_updated_at
    BEFORE UPDATE ON monitored_devices
    FOR EACH ROW EXECUTE FUNCTION monitor_touch_updated_at();
