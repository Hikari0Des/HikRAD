-- 0232_alert_events — Phase 3, Agent 3. Contract C1-C / C5 / FR-36.
-- One row per rule fire, with the per-channel delivery results (C5: "every fire
-- → alert_events row with delivery results"). Also the persistence layer behind
-- the in-app notifications feed (SSE live/notifications reads recent rows).
CREATE TABLE alert_events (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    rule_id    uuid REFERENCES alert_rules(id) ON DELETE SET NULL,
    at         timestamptz NOT NULL DEFAULT now(),
    -- 'firing' when the condition became true, 'resolved' for the all-clear
    -- (e.g. nas_up after nas_down). Digest rules use 'firing'.
    state      text NOT NULL DEFAULT 'firing' CHECK (state IN ('firing', 'resolved')),
    -- Rule type snapshot so the feed renders without joining a possibly-deleted
    -- rule, plus a short human summary shown in-app.
    type       text NOT NULL,
    summary    text NOT NULL DEFAULT '',
    -- Condition context (nas_id, device_id, percent, depth, figures for digests…).
    payload    jsonb NOT NULL DEFAULT '{}',
    -- Per-channel outcome: [{"channel":"telegram","ok":true,"detail":"…"}].
    deliveries jsonb NOT NULL DEFAULT '[]'
);

-- Feed + history reads are newest-first, optionally filtered by rule.
CREATE INDEX alert_events_at_idx      ON alert_events (at DESC);
CREATE INDEX alert_events_rule_at_idx ON alert_events (rule_id, at DESC);
