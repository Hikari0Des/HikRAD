# Phase v4-10 — Notification Center, tickets & Telegram bot

> **FRs:** FR-160 … FR-165 · **Migrations:** 0623–0628 · **Depends on:** v4-1, v4-9 (payment events) · **Blocks:** v4-11 (portal surfaces)
> **Owner sub-PRD:** 03 (monitoring/alerts) + 06 (support) · **Decisions committed at kickoff:** 66

## 1. Intent

HikRAD sends notifications; it does not let anyone *manage* them. There is no outbox, no delivery status, no template editing, no broadcast, no SMS channel, and no way for a subscriber to raise a problem inside the product at all — support happens on WhatsApp and phone calls, invisible to the system.

SAMM's Notification Center and ticket queue turn both into operable surfaces. This phase builds them on the existing delivery infrastructure rather than beside it.

## 2. Scope

**In:** the Notification Center's six tabs, the SMS channel, templates, rules and timers, broadcast, the outbox, support tickets, and the Telegram self-service bot.
**Not in:** the portal's ticket and preference screens (v4-11 consumes what this phase exposes), alert *rules* themselves (already built — this phase gives them a management surface).

## 3. Work

### 3.1 FR-160 / FR-163 — Notification Center (0623–0624)
Six tabs, one throttled queue underneath:
- **Overview** — live counts (queued, sending, sent today, failed, skipped, opted-out), channel state, recent feed.
- **Channels** — per-channel configuration and health.
- **Messages** — editable per-event templates.
- **Rules & timers** — per-rule enable/disable, timing (e.g. days before expiry), the **"ignore opt-out"** flag for genuinely transactional messages, and delivery throttling (batch size, tick interval).
- **Broadcast** — one-off sends to a subscriber set, through the **same** queue, never a side channel.
- **Outbox** — every queued and sent message with per-message status and failure reason.

`0623` adds `notification_outbox` (recipient, channel, event, template version, state, attempts, error, timestamps). `0624` adds `notification_templates` and `notification_rules`.

### 3.2 FR-161 — SMS channel (0625)
Generic HTTP gateway (configurable URL, method, parameter mapping) plus one named provider. Joins the existing email, Telegram and WhatsApp channels under one interface, with **all channel secrets encrypted at rest** via the existing crypto service — verified, not assumed. Per-subscriber, per-channel opt-in stored in `0625`.

### 3.3 FR-162 — Templates
Per-event templates, trilingual, editable in the operator's own voice. Events: renewal reminder, expiry notice, quota warning, payment receipt, plan renewed, payment-ticket state changes, and staff alert notifications. Template changes are versioned so the outbox can show which version produced a given message.

**Every user-visible default string is trilingual from the same commit** — the CI i18n gate applies to templates exactly as to UI strings.

### 3.4 FR-164 — Support tickets (0626–0627)
`0626` `tickets` (number, subscriber, subject, priority `normal|moderate|critical`, status `open|in_progress|resolved|closed|withdrawn`, timestamps) and `0627` `ticket_messages` (author, body, **`internal` flag**).

- Queue with filter chips by status and priority, search by number, subject or customer, and an open-count sidebar badge.
- Created by admins for any customer, or by subscribers from the portal (v4-11) and the bot.
- **Public replies vs internal staff-only notes** — the internal flag is a data-level distinction, and a test asserts an internal note is never serialised to a subscriber-facing response. This is the one place in the feature where a bug is a customer-visible embarrassment.
- Subscribers may withdraw their own tickets; withdrawn tickets are hidden by default.
- Scoped by the existing `auth.ScopeFilter` — an agent sees only their own subscribers' tickets.

### 3.5 FR-165 — Telegram bot (0628)
A self-service bot: check usage, view invoices, submit and follow tickets, receive notifications. Account linking via a one-time code issued from the portal, stored in `0628`.

Internet-dependent by nature: the bot degrades gracefully, queues nothing indefinitely, and its absence never affects any daily operation (NFR-7).

## 4. Frozen contracts

- One queue. Broadcast, transactional, and alert messages all traverse it — no side channel.
- `notification_outbox` row shape and its state machine.
- Internal ticket notes are never serialised into a subscriber-facing payload. Asserted by test.
- Channel secrets go through the existing encryption service; no new secret storage path.
- Opt-out is honoured except where a rule explicitly sets "ignore opt-out", and that flag is visible in the rule UI — never a hidden default.

## 5. Acceptance gate

| # | Item | Proof |
|---|---|---|
| 1 | One queue | Broadcast, transactional and alert messages all appear in the same outbox |
| 2 | Outbox truthful | Force a channel failure; the row shows the real reason, not a generic error |
| 3 | SMS channel | Message delivered through a stub HTTP gateway; secret encrypted at rest, verified in the DB |
| 4 | Opt-out honoured | Opted-out subscriber receives no non-transactional message; "ignore opt-out" rule still delivers |
| 5 | Templates trilingual | i18n gate green; every default template exists in en/ar/ku |
| 6 | Template versioning | Outbox row identifies the version that produced it |
| 7 | Throttling | Batch size and tick interval respected under a 1,000-message broadcast |
| 8 | Ticket lifecycle | All five statuses and three priorities; withdrawal hides by default |
| 9 | **Internal notes never leak** | Test asserts no subscriber-facing response contains an internal message, on every endpoint |
| 10 | Ticket scoping | An agent sees only their own subscribers' tickets |
| 11 | Bot | Link an account by one-time code; usage, invoices, ticket create and follow all work |
| 12 | Bot degrades | Telegram unreachable; no daily operation affected, no unbounded queue growth |
| 13 | Migrations linear | 0623–0628 clean |

## 6. Commit plan

1. `feat(notify): outbox table and unified throttled queue (FR-160, 0623)`
2. `feat(notify): templates and rules with versioning (FR-162, 0624)`
3. `feat(notify): SMS channel with encrypted secrets and per-channel opt-in (FR-161, 0625)`
4. `feat(panel): Notification Center — six tabs (FR-160, FR-163)`
5. `feat(support): tickets and messages with internal-note separation (FR-164, 0626-0627)`
6. `feat(panel): ticket queue, filters, replies, internal notes, open badge (FR-164)`
7. `feat(bot): Telegram self-service bot with account linking (FR-165, 0628)`
8. `docs: v4-10 gate result`

## 7. Kickoff prompt

```
Execute phase v4-10 (Notification Center, tickets & Telegram bot) of the HikRAD v4 program,
solo and sequentially, start to finish, in this repo.

Read first:
  - docs/v4/00-v4-master-plan.md           (Decision 66)
  - docs/v4/phases/phase-v4-10-notifications-support/00-phase.md   (this brief)
  - docs/prd/03-lossless-accounting-live-monitoring.md  (FR-36 alerts, FR-55 WhatsApp)
  - backend/internal/ — the existing notification delivery, alert routing, quiet hours,
    retry and delivery-isolation infrastructure

Deliver FR-160 through FR-165. Migrations 0623-0628; confirm the repo maximum first.

Build ON the existing delivery infrastructure, not beside it. HikRAD already has alert
routing, quiet hours, retry, and delivery isolation. This phase adds the management surface
those have never had, plus the SMS channel, plus tickets, plus the bot.

Binding rules:
  - ONE QUEUE. Broadcast, transactional and alert messages all traverse the same throttled
    queue and all appear in the same outbox. No side channel, no "just this once" direct
    send (gate item 1).
  - The outbox must be TRUTHFUL. When a channel fails, the row shows the real reason. A
    generic "failed" trains operators to ignore the screen (item 2).
  - INTERNAL TICKET NOTES MUST NEVER REACH A SUBSCRIBER. This is a data-level distinction
    with a test on EVERY subscriber-facing endpoint (item 9), not a UI filter. A bug here
    is a customer-visible embarrassment, which is a different severity from a display bug.
  - Channel secrets go through the EXISTING encryption service. Verify encryption at rest
    by inspecting the stored value (item 3) — do not assume it.
  - Every default template ships trilingual in the same commit. The i18n CI gate applies to
    templates exactly as it does to UI strings.
  - Opt-out is honoured unless a rule explicitly sets "ignore opt-out", and that flag is
    VISIBLE in the rule UI. Never a hidden default.
  - The bot is internet-dependent: it must degrade gracefully and never affect any daily
    operation, and never grow an unbounded queue while Telegram is unreachable (NFR-7,
    item 12).

Tickets are scoped by the existing auth.ScopeFilter — an agent sees only their own
subscribers' tickets. Reuse it; do not write new scoping logic.

Finish with docs/history/gates/v4-10.md.
```
