-- 0120_nas — Phase 2, Agent 2 (RADIUS & NAS), migration range 0120–0129.
-- Contract C1-B / sub-PRD 02 §4 / FR-13: the NAS registry FreeRADIUS's client
-- list is driven from (FR-13.2). Shared secret and SNMP community are AES-GCM
-- sealed at rest via platform/crypto (C3, NFR-4) — never stored in cleartext.
CREATE TABLE nas (
    id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name               text NOT NULL,
    -- One row per physical NAS; the RADIUS client match and CoA target both
    -- key on this address, so it is unique (FR-13.4).
    ip                 inet NOT NULL UNIQUE,
    secret_enc         bytea NOT NULL,
    -- Service the NAS terminates; sessions.service (C's table) is derived from
    -- it (FR-58). PPPoE routers may still accept Hotspot logins for a flagged
    -- subscriber — that is a per-subscriber allowance, not a NAS attribute.
    type               text NOT NULL DEFAULT 'pppoe'
                         CHECK (type IN ('pppoe', 'hotspot')),
    -- Selects the vendor adapter (FR-17.2). MikroTik is the only certified
    -- vendor in v1; the column exists so adding one needs no schema change.
    vendor             text NOT NULL DEFAULT 'mikrotik',
    coa_port           int  NOT NULL DEFAULT 3799 CHECK (coa_port BETWEEN 1 AND 65535),
    snmp_community_enc bytea,
    -- RouterOS major version note ('6' | '7'): drives quirk handling and the
    -- FR-14.3 snippet variant. Nullable — unknown until Ali fills it in.
    ros_version        text,
    location           text NOT NULL DEFAULT '',
    enabled            boolean NOT NULL DEFAULT true,
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now()
);

-- FreeRADIUS matches incoming packets by source IP; index the lookup the
-- clients regeneration and the authorize known-NAS check both make.
CREATE INDEX nas_enabled_idx ON nas (enabled);

-- Namespaced touch-updated_at so it cannot collide with another agent's
-- identically-purposed function created in a lower migration range.
CREATE OR REPLACE FUNCTION radius_touch_updated_at() RETURNS trigger AS $$
BEGIN
    NEW.updated_at := now();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER nas_set_updated_at
    BEFORE UPDATE ON nas
    FOR EACH ROW EXECUTE FUNCTION radius_touch_updated_at();
