-- 0627 — v4-10, FR-161. Per-subscriber, per-channel opt-in for the channels
-- that don't already have one: sms (new this phase) and email. whatsapp
-- keeps its own dedicated `whatsapp_opt_in` column (0100) and default
-- semantics (explicit opt-IN required, FR-55.3) unchanged — this table is
-- deliberately not a second source of truth for it.
--
-- Absence of a row means opted-in: sms/email are utility-style channels
-- where an opt-OUT model is the norm for this market, matching how
-- renewal/receipt SMS already worked before this phase had any record of
-- consent at all. A subscriber (or an operator on their behalf) opts out
-- explicitly; the outbox honours that per the binding "opt-out is honoured
-- unless the rule says ignore_opt_out" rule.
--
-- Channel gateway credentials (the generic HTTP SMS config + the one named
-- provider preset) are NOT a new table: they go through the existing
-- platform.SetEncrypted/GetEncrypted settings-group path (key
-- "notifications.sms"), the same mechanism notifications.smtp/telegram/
-- whatsapp already use — no new secret storage path (binding rule).
CREATE TABLE subscriber_channel_opt_ins (
    subscriber_id uuid NOT NULL REFERENCES subscribers(id) ON DELETE CASCADE,
    channel       text NOT NULL CHECK (channel IN ('sms','email')),
    opted_in      boolean NOT NULL DEFAULT true,
    updated_at    timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (subscriber_id, channel)
);
