-- 0625 — v4-10 (Notification Center & unified queue), FR-160. Migration
-- budget was 0623-0628 per the phase brief; the repo's real max at kickoff
-- was already 0624 (v4-9's approvals) — the master plan's "next free
-- number, budgets are not reservations" rule moves the whole range up by
-- one, same renumbering every prior phase needed. This phase's range is
-- 0625-0630.
--
-- notification_outbox is the ONE queue every outbound message traverses —
-- broadcast, transactional (renewal reminder, receipt, ticket reply) and
-- staff alert alike (binding rule: no side channel, ever). Modeled on
-- coa_outbox (0614): a durable row per message with a real state machine,
-- drained by a dispatcher on its own tick, so the queue survives a restart
-- and every send is inspectable after the fact. Channel senders (telegram/
-- email/whatsapp/sms) land in 0627; the channel enum is declared complete
-- here so the outbox row shape never needs to change under them.
CREATE TABLE notification_outbox (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),

    -- Coarse provenance (display only, never filtering logic). event is the
    -- notification_templates/notification_rules key this row was rendered
    -- from ("" for a hand-typed broadcast, which carries no rule/template).
    source           text NOT NULL CHECK (source IN ('alert', 'rule', 'broadcast', 'ticket', 'bot')),
    source_id        uuid,
    event            text NOT NULL DEFAULT '',

    -- Recipient. subscriber_id/manager_id are kept for display/scoping —
    -- who this message was about; the deliverable contact (phone, email,
    -- telegram chat id) and the opt-out decision are resolved once at
    -- enqueue time and stored in address, which is what the dispatcher
    -- actually sends to.
    subscriber_id    uuid REFERENCES subscribers(id) ON DELETE SET NULL,
    manager_id       uuid REFERENCES managers(id) ON DELETE SET NULL,
    address          text NOT NULL DEFAULT '',

    channel          text NOT NULL CHECK (channel IN ('inapp','telegram','email','whatsapp','sms')),
    locale           text NOT NULL DEFAULT 'en',

    template_key     text NOT NULL DEFAULT '',
    template_version int,
    subject          text NOT NULL DEFAULT '',
    body             text NOT NULL,

    -- Opt-out is honoured unless the rule that produced this row explicitly
    -- set "ignore opt-out" (frozen contract: visible in the rule UI, never
    -- a hidden default). Recorded per-row so the outbox itself proves which
    -- rows bypassed it.
    ignore_opt_out   boolean NOT NULL DEFAULT false,

    state            text NOT NULL DEFAULT 'queued'
                       CHECK (state IN ('queued','sending','sent','failed','skipped')),
    attempt_count    int  NOT NULL DEFAULT 0,
    -- TRUTHFUL failure/skip reason (binding rule: never a generic "failed").
    -- last_error carries the real channel error string ("smtp not
    -- configured", "http 401: unauthorized", …); skip_reason is the small
    -- closed set of reasons a row is never even attempted (e.g. opted_out,
    -- no_contact, channel_disabled).
    last_error       text NOT NULL DEFAULT '',
    skip_reason      text NOT NULL DEFAULT '',

    -- Dispatcher pickup ordering / backoff pacing, same shape as coa_outbox.
    next_attempt_at  timestamptz NOT NULL DEFAULT now(),

    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now(),
    sent_at          timestamptz
);

-- The dispatcher's own poll: "every row ready to be attempted", oldest first.
CREATE INDEX notification_outbox_dispatch_idx ON notification_outbox (next_attempt_at)
    WHERE state = 'queued';

-- Overview tab counters + Outbox tab filters (FR-160/FR-163).
CREATE INDEX notification_outbox_state_idx ON notification_outbox (state, created_at DESC);
CREATE INDEX notification_outbox_event_idx ON notification_outbox (event);
CREATE INDEX notification_outbox_channel_idx ON notification_outbox (channel);
CREATE INDEX notification_outbox_subscriber_idx ON notification_outbox (subscriber_id) WHERE subscriber_id IS NOT NULL;
CREATE INDEX notification_outbox_source_idx ON notification_outbox (source, source_id);

CREATE OR REPLACE FUNCTION notify_touch_updated_at() RETURNS trigger AS $$
BEGIN
    NEW.updated_at := now();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER notification_outbox_set_updated_at
    BEFORE UPDATE ON notification_outbox
    FOR EACH ROW EXECUTE FUNCTION notify_touch_updated_at();
