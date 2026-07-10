-- 0121_ip_pools — Phase 2, Agent 2 (RADIUS & NAS), range 0120–0129.
-- Contract C1-B / FR-16: named IP pools returned as Framed-Pool at auth
-- (allocation happens on the MikroTik). `purpose` separates the active pool
-- from the expired walled-garden pool and the static-IP range (FR-16.1).
CREATE TABLE ip_pools (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name       text NOT NULL UNIQUE,
    -- One or more CIDR ranges. inet[] keeps each range first-class for the
    -- utilization math (size = sum of host counts) and static-IP membership
    -- validation (FR-16.2) without parsing a text blob.
    ranges     inet[] NOT NULL,
    purpose    text NOT NULL DEFAULT 'active'
                 CHECK (purpose IN ('active', 'expired', 'static')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TRIGGER ip_pools_set_updated_at
    BEFORE UPDATE ON ip_pools
    FOR EACH ROW EXECUTE FUNCTION radius_touch_updated_at();
