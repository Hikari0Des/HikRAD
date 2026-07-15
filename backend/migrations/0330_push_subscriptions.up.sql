-- 0330_push_subscriptions — Phase 4, Agent 2 (Accounting & Monitoring).
-- Contract C1-C / C4 / FR-54.4. One row per browser Push subscription, owned by
-- exactly one manager (panel surface) or one subscriber (portal surface) — never
-- both, enforced below. endpoint is globally unique (a re-subscribe from the
-- same browser/device upserts rather than duplicating). p256dh/auth are the
-- subscription's own ECDH public key + auth secret (RFC 8291), base64url as the
-- browser Push API returns them; never sealed with A's crypto since they are not
-- server secrets (they're published to the browser by design).
CREATE TABLE push_subscriptions (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    surface       text NOT NULL CHECK (surface IN ('panel', 'portal')),
    manager_id    uuid,
    subscriber_id uuid,
    endpoint      text NOT NULL UNIQUE,
    p256dh        text NOT NULL,
    auth          text NOT NULL,
    created_at    timestamptz NOT NULL DEFAULT now(),
    CHECK (
        (surface = 'panel'  AND manager_id    IS NOT NULL AND subscriber_id IS NULL) OR
        (surface = 'portal' AND subscriber_id IS NOT NULL AND manager_id    IS NULL)
    )
);

CREATE INDEX push_subscriptions_manager_idx    ON push_subscriptions (manager_id)    WHERE manager_id IS NOT NULL;
CREATE INDEX push_subscriptions_subscriber_idx ON push_subscriptions (subscriber_id) WHERE subscriber_id IS NOT NULL;
