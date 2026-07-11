-- 0231_alert_rules — Phase 3, Agent 3. Contract C1-C / C5 / FR-36.
-- Editable alert rules evaluated by the alerts engine (internal/monitorsvc).
-- `type` and the channel enum are frozen (C5): the code matches on the type
-- string, never on a rule name. threshold/channels/quiet_hours are jsonb so a
-- rule's shape can vary by type without a schema change.
CREATE TABLE alert_rules (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name        text NOT NULL,
    -- Frozen rule types (C5, device_* added 2026-07-11 for FR-60).
    type        text NOT NULL CHECK (type IN (
                    'nas_down', 'nas_up', 'device_down', 'device_up',
                    'radius_reject_spike', 'acct_backlog', 'disk_low',
                    'expiring_digest', 'agent_balance_low')),
    -- Type-specific config, e.g. {"percent":85} for disk_low, {"depth":1000} for
    -- acct_backlog, {"days":3} for expiring_digest.
    threshold   jsonb NOT NULL DEFAULT '{}',
    -- Ordered channel list from the frozen enum inapp|telegram|email|whatsapp.
    channels    jsonb NOT NULL DEFAULT '["inapp"]',
    -- Per-rule WhatsApp recipient numbers (FR-36.2 / sub-PRD 03) and any
    -- extra e-mail/telegram-chat overrides; {} → use the settings defaults.
    recipients  jsonb NOT NULL DEFAULT '{}',
    -- {"start":"22:00","end":"07:00"} in Asia/Baghdad; null → always alert. Quiet
    -- hours suppress telegram/email/whatsapp but never in-app (C5 / gate item 6).
    quiet_hours jsonb,
    -- Minimum seconds between two fires of this rule (alert-storm damping).
    cooldown_s  integer NOT NULL DEFAULT 300 CHECK (cooldown_s >= 0),
    enabled     boolean NOT NULL DEFAULT true,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX alert_rules_enabled_idx ON alert_rules (enabled, type);

CREATE OR REPLACE FUNCTION monitor_touch_updated_at() RETURNS trigger AS $$
BEGIN
    NEW.updated_at := now();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER alert_rules_set_updated_at
    BEFORE UPDATE ON alert_rules
    FOR EACH ROW EXECUTE FUNCTION monitor_touch_updated_at();

-- Default rule set seeded (task 4 DoD: "NAS down, disk 85%, backlog, invariant
-- broken"). in-app + telegram so they light the panel and reach Omar's phone out
-- of the box; operators tune thresholds/channels/quiet-hours in the panel.
INSERT INTO alert_rules (name, type, threshold, channels) VALUES
    ('NAS down',                'nas_down',    '{}',              '["inapp","telegram"]'),
    ('Disk almost full (85%)',  'disk_low',    '{"percent":85}',  '["inapp","telegram"]'),
    ('Accounting backlog',      'acct_backlog','{"depth":1000}',  '["inapp","telegram"]'),
    ('Pipeline invariant broken','acct_backlog','{"invariant":true}', '["inapp","telegram"]')
ON CONFLICT DO NOTHING;
