-- 0632 — v4-11 (Self-service guest registration FR-169 + OTP login FR-172),
-- Decision 64's sibling feature. One passcode table serves BOTH flows — the
-- phase brief's own instruction ("OTP login... reusing FR-169's passcode
-- machinery") ruled out a second, parallel implementation: 'register' binds
-- a code to a not-yet-existing visitor's phone number; 'otp_login' binds one
-- to an EXISTING passwordless (has_password=false) subscriber, closing the
-- long-open known-issues row dated 2026-07-16.
CREATE TABLE guest_passcodes (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    purpose        text NOT NULL CHECK (purpose IN ('register','otp_login')),
    phone          text NOT NULL,
    subscriber_id  uuid REFERENCES subscribers(id) ON DELETE CASCADE,
    code_hash      bytea NOT NULL,          -- sha256(code); the plaintext never touches storage or logs
    mac            text NOT NULL DEFAULT '', -- the device MAC the visitor cannot fake from a browser (FR-169)
    name           text NOT NULL DEFAULT '', -- register only: held until the account is actually created
    sent_at        timestamptz NOT NULL DEFAULT now(), -- validity counts from HERE, never from first read (FR-169)
    expires_at     timestamptz NOT NULL,
    consumed_at    timestamptz,
    attempt_count  int NOT NULL DEFAULT 0,   -- wrong-code guesses against this row
    created_at     timestamptz NOT NULL DEFAULT now(),
    CHECK ((purpose = 'register' AND subscriber_id IS NULL) OR (purpose = 'otp_login' AND subscriber_id IS NOT NULL))
);

-- "One live passcode per mobile number" (FR-169 abuse control), enforced
-- structurally, not just in application logic: a second request while a code
-- for the same (purpose, phone) is still outstanding must find this existing
-- row. Consumed rows fall out of the partial index so a legitimate later
-- request (after consumption, or after the cooldown past expiry) can insert
-- a fresh one.
CREATE UNIQUE INDEX guest_passcodes_live_idx ON guest_passcodes (purpose, phone) WHERE consumed_at IS NULL;
CREATE INDEX guest_passcodes_mac_idx ON guest_passcodes (mac, created_at DESC);

-- Every send/verify attempt, recorded against the device MAC independent of
-- whatever guest_passcodes row exists at the time (that row can be replaced
-- by a later one) — this table is what the daily-volume cap and per-MAC
-- abuse signal both read.
CREATE TABLE guest_passcode_attempts (
    id      uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    purpose text NOT NULL CHECK (purpose IN ('register','otp_login')),
    phone   text NOT NULL,
    mac     text NOT NULL DEFAULT '',
    action  text NOT NULL CHECK (action IN ('send','verify_ok','verify_fail','cooldown_refused','duplicate_refused')),
    at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX guest_passcode_attempts_mac_idx ON guest_passcode_attempts (mac, at DESC);
CREATE INDEX guest_passcode_attempts_action_idx ON guest_passcode_attempts (purpose, action, at DESC);

-- Config (generic settings group, same reuse-the-proven-mechanism decision
-- as portal_auto_login — see internal/platform/setupapi/settings_api.go).
-- Off by default; default_profile_id is deliberately left unset so a fresh
-- install cannot silently create real hotspot accounts against a
-- nonexistent or wrong plan — the register endpoint refuses with a clear
-- "not configured" error until an operator sets one (setup-order
-- prerequisite, phase brief §3.4).
INSERT INTO settings (key, value) VALUES
    ('guest_registration.enabled',              'false'),
    ('guest_registration.daily_cap',            '100'),
    ('guest_registration.passcode_ttl_minutes', '10'),
    ('guest_registration.cooldown_minutes',     '5'),
    ('guest_registration.default_profile_id',   '""'),
    ('guest_registration.page_text',            '{}')
ON CONFLICT (key) DO NOTHING;

-- notification_outbox.source's CHECK (0625) predates this phase and does
-- not yet know about passcode sends; widen it rather than misusing an
-- existing value ('rule' implies EnqueueEvent's subscriber-contact/opt-out
-- model, which notify.EnqueuePasscode deliberately does not go through).
ALTER TABLE notification_outbox DROP CONSTRAINT notification_outbox_source_check;
ALTER TABLE notification_outbox ADD CONSTRAINT notification_outbox_source_check
    CHECK (source IN ('alert', 'rule', 'broadcast', 'ticket', 'bot', 'guest_passcode'));

-- notify integration (FR-169 §4 frozen contract: "passcode issuance goes
-- through v4-10's queue like every other message"). Two new event keys,
-- trilingual default templates, and rules an operator can re-point at a
-- different channel set — same shape 0626 seeded for v4-10's own events.
-- {{code}} is the one placeholder both templates need. Channels default to
-- sms+whatsapp (no inapp: there is no logged-in session to show it in yet).
INSERT INTO notification_rules (event_key, name, enabled, channels, days_before, ignore_opt_out) VALUES
    ('guest_registration_passcode', 'Guest registration passcode', true, '["sms","whatsapp"]', NULL, true),
    ('otp_login_passcode',          'OTP login passcode',          true, '["sms","whatsapp"]', NULL, true)
ON CONFLICT DO NOTHING;

INSERT INTO notification_templates (event_key, locale, version, subject, body, is_default) VALUES
    ('guest_registration_passcode', 'en', 1, '', 'Your HikRAD verification code is {{code}}. It expires in {{ttl_minutes}} minutes.', true),
    ('guest_registration_passcode', 'ar', 1, '', 'رمز التحقق الخاص بك هو {{code}}. تنتهي صلاحيته خلال {{ttl_minutes}} دقيقة.', true),
    ('guest_registration_passcode', 'ku', 1, '', 'کۆدی پشتڕاستکردنەوەت {{code}}ـە. لە ماوەی {{ttl_minutes}} خولەکدا بەسەردەچێت.', true),

    ('otp_login_passcode', 'en', 1, '', 'Your HikRAD login code is {{code}}. It expires in {{ttl_minutes}} minutes.', true),
    ('otp_login_passcode', 'ar', 1, '', 'رمز الدخول الخاص بك هو {{code}}. تنتهي صلاحيته خلال {{ttl_minutes}} دقيقة.', true),
    ('otp_login_passcode', 'ku', 1, '', 'کۆدی چوونەژوورەوەت {{code}}ـە. لە ماوەی {{ttl_minutes}} خولەکدا بەسەردەچێت.', true)
ON CONFLICT DO NOTHING;
