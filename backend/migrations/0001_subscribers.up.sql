-- 0001_subscribers — Phase-1 contract C6 (owner: Agent 3 / Backend Business,
-- migration range 0001–0009). Later columns arrive in later phases.

CREATE EXTENSION IF NOT EXISTS citext;

CREATE TABLE subscribers (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    username     citext NOT NULL UNIQUE,
    password_enc bytea,
    name         text,
    phone        text,
    status       text NOT NULL DEFAULT 'active',
    profile_id   uuid,
    expires_at   timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now()
);
