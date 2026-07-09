-- 0002_profiles — Phase-1 contract C6 (owner: Agent 3 / Backend Business,
-- migration range 0001–0009). Later columns arrive in later phases.

CREATE TABLE profiles (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name           text NOT NULL,
    price_iqd      bigint NOT NULL,
    duration_days  int NOT NULL,
    rate_down_kbps int NOT NULL,
    rate_up_kbps   int NOT NULL,
    created_at     timestamptz NOT NULL DEFAULT now()
);
