-- 0303_gateway_configs — Phase 4, Agent 3, range 0300–0309.
-- Contract C1-D / C3, FR-23.1. Per-gateway enable/config; merchant credentials
-- are sealed with A's crypto (NFR-4.2/4.3), never stored in the clear. mode
-- distinguishes a gateway's mock/sandbox posture from live (the mock adapter
-- itself is always registered in code regardless of this table — it ships
-- disabled here by default so it never appears as a live payment option
-- unless an operator explicitly enables it for demo/CI).
CREATE TABLE gateway_configs (
    gateway    text PRIMARY KEY,
    enabled    boolean NOT NULL DEFAULT false,
    mode       text NOT NULL DEFAULT 'live', -- live|mock
    creds_enc  bytea,
    updated_at timestamptz NOT NULL DEFAULT now()
);

-- Mock always exists as a row so CI/demo can flip it on without a first
-- POST (dev/demo convenience only; still defaults disabled).
INSERT INTO gateway_configs (gateway, enabled, mode) VALUES ('mock', false, 'mock');
