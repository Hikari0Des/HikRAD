-- 0609 — v4-6 (Router manager I), FR-140. See 0607 for the migration-number note.
--
-- device_config_cache: the device worker's last-read router state, one row per
-- (nas_id, object_class) — object_class is one cache slot per read-only device
-- tab ("overview" | "interfaces" | "general" | "bridge" | "vlan" | "ip" | "dns").
-- This is what makes every FR-140 tab render instantly from cache and refresh
-- only on an explicit action, never a synchronous router call in the request
-- path (gate items 2/3). `hash` reuses the same deterministic-digest approach
-- autosetup_api.go's planHash already established (sha256 hex of the canonical
-- payload) so a tab can tell the operator "unchanged since last read" — not a
-- new hashing scheme, the same technique applied to a read-model cache instead
-- of a preview/apply round trip.
CREATE TABLE device_config_cache (
    nas_id       uuid NOT NULL,
    object_class text NOT NULL,
    data         jsonb NOT NULL,
    hash         text NOT NULL DEFAULT '',
    reachable    boolean NOT NULL DEFAULT true,
    read_at      timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (nas_id, object_class)
);
