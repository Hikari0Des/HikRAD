# Phase v4-8 — CoA outbox & dispatch

> **FRs:** FR-150, FR-151, FR-152 · **Migrations:** 0613–0614 · **Depends on:** v4-4 · **Blocks:** v4-12 (dashboard panel)
> **Owner sub-PRD:** 02 (RADIUS & NAS) + 03 (monitoring surface)

## 1. Intent

CoA is how HikRAD's hero flow works — renew a subscriber sitting in the expired pool and full speed returns without a re-dial. But today a CoA is fired inline and its outcome is not durably recorded, which means: a failure is invisible, a router that NACKs updates silently never applies the change, and a restart loses whatever was in flight. v4-4 multiplied the number of CoAs the system sends (window entry/exit, throttle, daily reset), which makes this the right moment to make the path durable and observable.

SAMM's CoA outbox is a screen operators actually watch. Ours should be too.

## 2. Scope

**In:** the durable outbox, the dispatcher with hybrid fallback, and the operator surface.
**Not in:** the enforcement logic that decides *what* to send (v4-4 owns that), the dashboard panel that summarises it (v4-12).

## 3. Work

### 3.1 FR-150 — Durable outbox (0613–0614)
- **0613** — `coa_outbox`: subscriber, NAS, session id, action (`update` | `disconnect`), the **exact attribute set** to send, reason, state (`pending` → `sent` → `done` | `nack` | `failed`), attempt count, last error, timestamps, and the requesting manager where applicable.
- **0614** — `coa_attempts`: one row per dispatch attempt with the response received, so "why did this fail" is answerable after the fact rather than from logs that have rotated.

Every enforcement action, renewal, plan change, disconnect, and window transition **enqueues** rather than sends. Nothing dispatches inline. The queue survives restart by construction.

### 3.2 FR-151 — Hybrid dispatch
Dispatcher runs on the worker tick:
1. Send **CoA-Update** with the intended attributes.
2. On NACK, retry up to the configured limit (setting `coa_retry_max`, default 3).
3. On exhausting retries, **automatically fall back to Disconnect-Request** — the subscriber reconnects and picks up the new settings on re-auth.

This is what makes speed changes work across RouterOS versions that reject in-place updates. The fallback is recorded in the row so an operator can see it happened; it is never silent.

Timeouts, the CoA port, and the retry limit come from the existing hot-reloadable settings — not new ones.

### 3.3 FR-152 — Operator surface
A CoA Outbox screen: six counters (pending, sent, nack, done, failed, total) · filter by status and action · search by username, NAS, or reason · auto-refresh · per-row **retry**, **cancel**, and **inspect the exact RADIUS attributes sent and the response received** · sidebar badge counting pending · 24-hour failure count surfaced for v4-12's dashboard panel.

The inspect view matters more than it looks: it turns "CoA doesn't work on this router" from a support escalation into something the operator can diagnose themselves, which is the same reason the RADIUS debug tail exists.

### 3.4 Interaction with the reaper
The stale-session reaper and the outbox must not fight: a disconnect enqueued for a session the reaper has already closed resolves as `done` (no-op), not `failed`. Tested explicitly.

## 4. Frozen contracts

- `coa_outbox` row shape and its state machine: `pending → sent → {done | nack | failed}`, with `nack → pending` on retry and `nack → sent` on fallback. No other transitions.
- Enqueue is the **only** way to send a CoA. A direct dispatch call outside the dispatcher is a lint/review failure.
- Attribute sets are stored as sent, not regenerated at display time — the inspect view must show what actually went on the wire.
- Retry limit, timeout and port come from existing settings keys.

## 5. Acceptance gate

| # | Item | Proof |
|---|---|---|
| 1 | Nothing dispatches inline | Grep + review: every CoA path enqueues; harness confirms |
| 2 | Queue survives restart | Enqueue, kill the worker, restart; the CoA still sends |
| 3 | Hybrid fallback | Harness CoA listener NACKs every update; dispatcher falls back to Disconnect after the retry limit |
| 4 | Fallback is visible | The row shows the fallback occurred and why |
| 5 | Retry and cancel | Per-row retry re-queues; cancel stops an unsent row and cannot cancel a sent one |
| 6 | Inspect shows the wire | Attributes displayed match a packet capture byte-for-byte |
| 7 | Counters accurate | Six counters reconcile against the table under load |
| 8 | Reaper interaction | Disconnect for an already-reaped session resolves `done`, not `failed` |
| 9 | Hero flow intact | Renew an expired-pool subscriber; full speed restored without re-dial |
| 10 | Load | 500 queued CoAs drain without duplicate dispatch or lost rows |
| 11 | Migrations linear | 0613–0614 clean |

## 6. Commit plan

1. `feat(radius): durable CoA outbox and attempt log (FR-150, 0613-0614)`
2. `refactor(radius): all CoA callers enqueue instead of dispatching inline (FR-150)`
3. `feat(radius): dispatcher with retry and Disconnect-Request fallback (FR-151)`
4. `fix(radius): reaped-session disconnect resolves done, not failed (FR-150)`
5. `feat(panel): CoA outbox screen with counters, filters, retry/cancel/inspect (FR-152)`
6. `feat(panel): pending-CoA sidebar badge and 24h failure count (FR-152)`
7. `docs: v4-8 gate result`

## 7. Kickoff prompt

```
Execute phase v4-8 (CoA outbox & dispatch) of the HikRAD v4 program, solo and sequentially,
start to finish, in this repo.

Read first:
  - docs/v4/00-v4-master-plan.md
  - docs/v4/phases/phase-v4-8-coa-outbox/00-phase.md   (this brief — §4 is frozen)
  - docs/v4/phases/phase-v4-4-limits-engine/00-phase.md (what now enqueues CoAs)
  - backend/internal/radius/coa.go and internal/live/coa_action.go
  - backend/test/harness/ — it has a coa-listen mode you will need

Deliver FR-150, FR-151, FR-152. Migrations 0613-0614; confirm the repo maximum first.

Today CoAs are fired inline and their outcome is not durably recorded: failures are
invisible, a router that NACKs updates silently never applies the change, and a restart
loses whatever was in flight. v4-4 multiplied how many CoAs the system sends, so this is
the moment to fix it.

Binding rules:
  - ENQUEUE IS THE ONLY WAY TO SEND A CoA. Refactor every existing caller. A direct
    dispatch outside the dispatcher is a review failure (gate item 1).
  - Store the attribute set AS SENT and display that. Do not regenerate it at display time
    — the whole value of the inspect view is that it shows what actually went on the wire
    (gate item 6, verified against a packet capture).
  - The hybrid fallback (CoA-Update, then Disconnect-Request after the retry limit) is what
    makes speed changes work across RouterOS versions that reject in-place updates. It must
    be VISIBLE in the row, never silent (items 3 and 4). Use the harness's coa-listen mode
    set to NACK everything to prove it.
  - Retry limit, timeout and CoA port come from the EXISTING hot-reloadable settings. Do
    not introduce new ones.
  - Watch the reaper interaction: a disconnect enqueued for a session the reaper already
    closed must resolve `done` (no-op), not `failed` (item 8). Getting this wrong fills the
    operator's screen with false failures and trains them to ignore it.

The hero flow — renew an expired-pool subscriber and full speed returns with no re-dial —
must still work end to end (item 9). If this phase breaks that, nothing else it delivers
matters.

Finish with docs/history/gates/v4-8.md.
```
