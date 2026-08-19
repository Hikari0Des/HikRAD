-- 0614 — v4-8 (CoA outbox & dispatch), FR-150. Migration budget was 0613-0614
-- per the phase brief; the repo's real max at kickoff was 0613 (v4-7's last
-- migration), so this phase starts at 0614-0615 per the master plan's
-- linear-sequence rule (§6.2), one past its stated budget — same renumbering
-- v4-6/v4-7 themselves needed.
--
-- coa_outbox is the durable record of every CoA/Disconnect HikRAD ever means
-- to send. Nothing dispatches inline any more (see coa_enqueue.go) — every
-- enforcement action, renewal, plan change, disconnect and window transition
-- inserts a row here and the dispatcher (coa_dispatcher.go) drains it on its
-- own tick. The queue therefore survives a restart by construction: a row
-- left `pending` (never attempted) or `sent` (attempted, outcome unknown
-- because the process died mid-round-trip) is picked back up automatically.
--
-- action is the ORIGINAL requested operation and never changes — it is what
-- FR-152's action filter matches on. current_op is what is actually being
-- attempted right now, which for an `update` row becomes `disconnect` once
-- the hybrid fallback (FR-151) kicks in; fallback_used/fallback_reason make
-- that switch visible in the row itself, never silently (phase brief binding
-- rule, gate items 3/4) rather than a second row appearing from nowhere.
--
-- attributes stores the exact abstract intent/value set (vendor.Attr) this
-- row means to send for an `update` action — written once at enqueue time
-- and never regenerated at display time (frozen contract §4). The literal
-- wire-level bytes actually placed on each attempt are captured separately,
-- per attempt, in coa_attempts (0615) — that is what the inspect view proves
-- against a packet capture (gate item 6).
CREATE TABLE coa_outbox (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    subscriber_id   uuid REFERENCES subscribers(id) ON DELETE SET NULL,
    nas_id          uuid NOT NULL REFERENCES nas(id) ON DELETE CASCADE,
    acct_session_id text NOT NULL,
    username        text NOT NULL,
    framed_ip       text NOT NULL DEFAULT '',
    service         text NOT NULL DEFAULT 'pppoe',

    action          text NOT NULL CHECK (action IN ('update', 'disconnect')),
    current_op      text NOT NULL CHECK (current_op IN ('update', 'disconnect')),
    attributes      jsonb NOT NULL DEFAULT '[]',
    reason          text NOT NULL,
    -- The manager who triggered this row directly (panel disconnect button,
    -- retry). NULL for every worker/renewal-triggered row — there is no
    -- manager in that context, and that absence is itself meaningful (FR-152
    -- inspect view: "system" vs a named operator).
    requested_by    uuid REFERENCES managers(id) ON DELETE SET NULL,

    state           text NOT NULL DEFAULT 'pending'
                      CHECK (state IN ('pending', 'sent', 'done', 'nack', 'failed')),
    attempt_count   int  NOT NULL DEFAULT 0,
    last_error      text NOT NULL DEFAULT '',
    last_outcome    text NOT NULL DEFAULT '',

    fallback_used   boolean NOT NULL DEFAULT false,
    fallback_at     timestamptz,
    fallback_reason text NOT NULL DEFAULT '',

    -- Dispatcher pickup ordering / backoff pacing: a row is eligible for the
    -- next attempt once now() >= next_attempt_at. Defaults to "now" so a
    -- freshly enqueued row is picked up on the very next tick.
    next_attempt_at timestamptz NOT NULL DEFAULT now(),

    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    sent_at         timestamptz,
    done_at         timestamptz
);

-- The dispatcher's own poll: "every row ready to be attempted", oldest first.
CREATE INDEX coa_outbox_dispatch_idx ON coa_outbox (next_attempt_at)
    WHERE state IN ('pending');

-- FR-152 counters/filters/search: status+action filters and the sidebar's
-- pending badge both scan by state; created_at DESC is the list's default
-- order.
CREATE INDEX coa_outbox_state_idx ON coa_outbox (state, created_at DESC);
CREATE INDEX coa_outbox_action_idx ON coa_outbox (action);
CREATE INDEX coa_outbox_username_idx ON coa_outbox (username);
CREATE INDEX coa_outbox_nas_idx ON coa_outbox (nas_id);
CREATE INDEX coa_outbox_subscriber_idx ON coa_outbox (subscriber_id) WHERE subscriber_id IS NOT NULL;
-- 24h failure count for v4-12's dashboard pipeline-health panel (FR-152's
-- "surfaced for v4-12", FR-174).
CREATE INDEX coa_outbox_failed_recent_idx ON coa_outbox (state, done_at) WHERE state = 'failed';

CREATE TRIGGER coa_outbox_set_updated_at
    BEFORE UPDATE ON coa_outbox
    FOR EACH ROW EXECUTE FUNCTION radius_touch_updated_at();
