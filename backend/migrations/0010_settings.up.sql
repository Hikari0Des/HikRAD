-- 0010_settings — contract C6 (Phase 1, Agent A). Exactly the Phase-1 shape:
-- later columns (updated_by, etc.) arrive in later phases, do not pre-add.
CREATE TABLE settings (
    key        text PRIMARY KEY,
    value      jsonb NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);

-- v1 defaults (FR-53.2 subset): locale group plus empty notification groups.
INSERT INTO settings (key, value) VALUES
    ('locale.timezone',        '"Asia/Baghdad"'),
    ('locale.currency',        '"IQD"'),
    ('notifications.smtp',     '{}'),
    ('notifications.telegram', '{}')
ON CONFLICT (key) DO NOTHING;
