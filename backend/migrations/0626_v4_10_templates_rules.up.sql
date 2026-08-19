-- 0626 — v4-10, FR-162. notification_templates (versioned, trilingual) and
-- notification_rules (per-event enable/timing/channels/ignore-opt-out).
--
-- Templates are append-only, same discipline as the journal (0617): editing
-- a template inserts the NEXT version rather than rewriting history, so an
-- outbox row's template_version always identifies the exact text that
-- produced it (gate item 6) even after an operator edits the wording later.
CREATE TABLE notification_templates (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    event_key   text NOT NULL,
    locale      text NOT NULL CHECK (locale IN ('en','ar','ku')),
    version     int  NOT NULL CHECK (version > 0),
    subject     text NOT NULL DEFAULT '',
    body        text NOT NULL,
    is_default  boolean NOT NULL DEFAULT false,
    created_at  timestamptz NOT NULL DEFAULT now(),
    created_by  uuid REFERENCES managers(id) ON DELETE SET NULL,
    UNIQUE (event_key, locale, version)
);

-- "The current template for (event, locale)" = max version — this is what
-- render() and the Messages tab both read.
CREATE INDEX notification_templates_current_idx ON notification_templates (event_key, locale, version DESC);

CREATE OR REPLACE FUNCTION notify_immutable() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION '% is append-only: % is not permitted', TG_TABLE_NAME, TG_OP
        USING ERRCODE = 'restrict_violation';
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER notification_templates_no_update BEFORE UPDATE ON notification_templates FOR EACH ROW EXECUTE FUNCTION notify_immutable();
CREATE TRIGGER notification_templates_no_delete BEFORE DELETE ON notification_templates FOR EACH ROW EXECUTE FUNCTION notify_immutable();
REVOKE UPDATE, DELETE ON notification_templates FROM PUBLIC;

-- notification_rules: one row per event this phase knows how to send.
-- event_key is the frozen join to notification_templates.event_key and to
-- notify.EnqueueEvent's callers (billing renewal/receipt/ticket events,
-- the expiry/quota scanner, the alerts engine's staff_alert channel).
CREATE TABLE notification_rules (
    id                    uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    event_key             text NOT NULL UNIQUE,
    name                  text NOT NULL,
    enabled               boolean NOT NULL DEFAULT true,
    channels              jsonb NOT NULL DEFAULT '["inapp"]',
    -- Timing for the scanner-driven events (renewal_reminder counts down to
    -- expiry, expiry_notice/quota_warning fire on the day the condition is
    -- true); NULL for event-driven rules that have no "days before" concept
    -- (payment_receipt, plan_renewed, payment_ticket_state, staff_alert).
    days_before           int,
    -- Frozen contract: opt-out is honoured UNLESS this is explicitly set —
    -- and it must render as a visible toggle in the Rules & timers tab,
    -- never a hidden default (binding rule).
    ignore_opt_out        boolean NOT NULL DEFAULT false,
    batch_size            int NOT NULL DEFAULT 50 CHECK (batch_size > 0),
    tick_interval_seconds int NOT NULL DEFAULT 5 CHECK (tick_interval_seconds > 0),
    created_at            timestamptz NOT NULL DEFAULT now(),
    updated_at            timestamptz NOT NULL DEFAULT now()
);

CREATE TRIGGER notification_rules_set_updated_at
    BEFORE UPDATE ON notification_rules
    FOR EACH ROW EXECUTE FUNCTION notify_touch_updated_at();

-- Seeded one rule per event so the Rules & timers tab has real, editable
-- rows from day one (mirrors alert_rules' seeded defaults, 0231).
-- ignore_opt_out=true is set only for genuinely transactional events (a
-- receipt/ticket-status update for money already in motion is not
-- optional marketing) — every promotional/reminder event defaults to
-- honouring opt-out, per the binding rule.
INSERT INTO notification_rules (event_key, name, enabled, channels, days_before, ignore_opt_out) VALUES
    ('renewal_reminder',     'Renewal reminder',              true, '["whatsapp","sms","inapp"]', 3,    false),
    ('expiry_notice',        'Expiry notice',                 true, '["whatsapp","sms","inapp"]', 0,    false),
    ('quota_warning',        'Quota warning (90%)',           true, '["whatsapp","sms","inapp"]', NULL, false),
    ('payment_receipt',      'Payment receipt',               true, '["whatsapp","email","inapp"]', NULL, true),
    ('plan_renewed',         'Plan renewed',                  true, '["whatsapp","inapp"]',       NULL, true),
    ('payment_ticket_state', 'Payment ticket status changed', true, '["whatsapp","inapp"]',       NULL, true),
    ('staff_alert',          'Staff alert notifications',     true, '["inapp","telegram"]',       NULL, true)
ON CONFLICT DO NOTHING;

-- Default templates, trilingual (en/ar/ku), version 1, marked is_default —
-- the i18n CI gate applies to these exactly as to UI strings (binding
-- rule): every event ships with all three locales in this same commit.
INSERT INTO notification_templates (event_key, locale, version, subject, body, is_default) VALUES
    ('renewal_reminder', 'en', 1, 'Renewal reminder',
        'Hi {{name}}, your {{plan}} plan expires in {{days_left}} day(s). Renew now to avoid interruption.', true),
    ('renewal_reminder', 'ar', 1, 'تذكير بالتجديد',
        'مرحباً {{name}}، ستنتهي صلاحية باقة {{plan}} خلال {{days_left}} يوم. جدد الآن لتجنب الانقطاع.', true),
    ('renewal_reminder', 'ku', 1, 'بیرخستنەوەی نوێکردنەوە',
        'سڵاو {{name}}، پلانی {{plan}} لە {{days_left}} ڕۆژدا بەسەردەچێت. ئێستا نوێی بکەرەوە بۆ ئەوەی وقفە ڕوونەدات.', true),

    ('expiry_notice', 'en', 1, 'Your plan has expired',
        'Hi {{name}}, your {{plan}} plan has expired. Renew now to restore full speed.', true),
    ('expiry_notice', 'ar', 1, 'انتهت صلاحية باقتك',
        'مرحباً {{name}}، انتهت صلاحية باقة {{plan}}. جدد الآن لاستعادة السرعة الكاملة.', true),
    ('expiry_notice', 'ku', 1, 'پلانەکەت بەسەرچووە',
        'سڵاو {{name}}، پلانی {{plan}} بەسەرچووە. ئێستا نوێی بکەرەوە بۆ گەڕاندنەوەی خێرایی تەواو.', true),

    ('quota_warning', 'en', 1, 'Data quota warning',
        'Hi {{name}}, you have used 90% of your {{plan}} plan''s data quota.', true),
    ('quota_warning', 'ar', 1, 'تنبيه استهلاك الباقة',
        'مرحباً {{name}}، لقد استهلكت 90% من بيانات باقة {{plan}}.', true),
    ('quota_warning', 'ku', 1, 'ئاگاداری کۆتای داتا',
        'سڵاو {{name}}، تۆ 90% ی داتای پلانی {{plan}} بەکارهێناوە.', true),

    ('payment_receipt', 'en', 1, 'Payment received',
        'Thank you {{name}}. We received {{amount}} {{currency}} (receipt {{receipt_no}}). New expiry: {{expires_at}}.', true),
    ('payment_receipt', 'ar', 1, 'تم استلام الدفعة',
        'شكراً {{name}}. استلمنا {{amount}} {{currency}} (إيصال {{receipt_no}}). تاريخ الانتهاء الجديد: {{expires_at}}.', true),
    ('payment_receipt', 'ku', 1, 'پارەدان وەرگیرا',
        'سوپاس {{name}}. ئێمە {{amount}} {{currency}} وەرگرت (پسوولە {{receipt_no}}). بەرواری کۆتایی نوێ: {{expires_at}}.', true),

    ('plan_renewed', 'en', 1, 'Plan renewed',
        'Hi {{name}}, your {{plan}} plan has been renewed. New expiry: {{expires_at}}.', true),
    ('plan_renewed', 'ar', 1, 'تم تجديد الباقة',
        'مرحباً {{name}}، تم تجديد باقة {{plan}}. تاريخ الانتهاء الجديد: {{expires_at}}.', true),
    ('plan_renewed', 'ku', 1, 'پلان نوێکرایەوە',
        'سڵاو {{name}}، پلانی {{plan}} نوێکرایەوە. بەرواری کۆتایی نوێ: {{expires_at}}.', true),

    ('payment_ticket_state', 'en', 1, 'Payment update',
        'Hi {{name}}, your payment ticket is now {{status}}.', true),
    ('payment_ticket_state', 'ar', 1, 'تحديث حالة الدفع',
        'مرحباً {{name}}، حالة طلب الدفع الخاص بك أصبحت الآن {{status}}.', true),
    ('payment_ticket_state', 'ku', 1, 'نوێکردنەوەی پارەدان',
        'سڵاو {{name}}، دۆسیەی پارەدانەکەت ئێستا بووە بە {{status}}.', true),

    ('staff_alert', 'en', 1, 'HikRAD alert', '{{summary}}', true),
    ('staff_alert', 'ar', 1, 'تنبيه HikRAD', '{{summary}}', true),
    ('staff_alert', 'ku', 1, 'ئاگاداری HikRAD', '{{summary}}', true)
ON CONFLICT DO NOTHING;
