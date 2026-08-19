-- 0630 — v4-10 (Telegram self-service bot), FR-165. Account linking: a
-- one-time code is issued (this phase's backend endpoint; the portal button
-- that calls it is v4-11's UI) and consumed by the bot when the subscriber
-- sends it in a Telegram chat, binding that chat_id to their subscriber_id.
-- One row per subscriber (a re-link overwrites, never accumulates rows) —
-- linking never proceeds without an exact, unexpired code match, and an
-- expired/consumed code cannot be reused (link_code cleared on success).
CREATE TABLE telegram_links (
    id                    uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    subscriber_id         uuid NOT NULL UNIQUE REFERENCES subscribers(id) ON DELETE CASCADE,
    chat_id               text UNIQUE,
    link_code             text,
    link_code_expires_at  timestamptz,
    linked_at             timestamptz,
    created_at            timestamptz NOT NULL DEFAULT now(),
    updated_at            timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX telegram_links_code_idx ON telegram_links (link_code) WHERE link_code IS NOT NULL;
CREATE INDEX telegram_links_chat_idx ON telegram_links (chat_id) WHERE chat_id IS NOT NULL;

CREATE TRIGGER telegram_links_set_updated_at
    BEFORE UPDATE ON telegram_links
    FOR EACH ROW EXECUTE FUNCTION notify_touch_updated_at();
