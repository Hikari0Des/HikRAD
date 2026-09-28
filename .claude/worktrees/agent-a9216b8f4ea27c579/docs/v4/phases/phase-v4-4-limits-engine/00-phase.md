# Phase v4-4 — Limits engine (SAMM parity)

> **FRs:** FR-128 … FR-132 · **Migrations:** 0597–0600 · **Depends on:** v4-1 · **Blocks:** v4-5, v4-8, v4-11, v4-12
> **Owner sub-PRD:** 04 (subscribers/profiles) + 02 (auth-time enforcement), split FR-64-style
> **Amends the frozen RADIUS reply contract** — see §4.

## 1. Intent

HikRAD enforces two limits: expiry and data quota. SAMM enforces four — expiry, quota, **uptime**, and **daily usage** — each with a configurable exhaustion action, plus **speed windows** that override the plan rate by time of day. This is the most visible feature gap in a side-by-side demo, and hotspot operators in particular sell on daily caps and night-time boosts.

This phase is the product's centre of gravity. Get the evaluation order and the CoA interaction right here and phases v4-8, v4-11 and v4-12 inherit correct behaviour.

## 2. Scope

**In:** the four-limit model, the unified exhaustion-action set, speed windows, and the enforcement worker that applies them live.
**Not in:** the CoA outbox itself (v4-8 — this phase enqueues through the existing CoA path and v4-8 makes that path durable), the subscriber-side counter split (v4-5), reports (v4-12).

## 3. Work

### 3.1 Data model (migrations 0597–0600)
- **0597** — `profiles` gains `uptime_limit_seconds`, `daily_limit_bytes`, `daily_reset_at` (time of day), and an `exhaust_action` + `exhaust_target` pair **per limit** (four pairs, not one shared).
- **0598** — `subscriber_counters`: the resettable per-limit state (quota used, uptime used, daily used, daily period start), separate from lifetime totals. v4-5 completes the split; this phase creates the table it needs.
- **0599** — `profile_speed_windows`: profile id, days-of-week mask, start and end time, download and upload rate, enabled.
- **0600** — seed plans (FR-132), inserted only on a database with zero profiles so an existing install is untouched.

### 3.2 FR-128 / FR-129 — Uptime and daily limits
- **Uptime** accumulates from accounting session time; exhaustion behaves like quota.
- **Daily** accumulates within the current daily period, resets at `daily_reset_at` in the **server timezone**, and its default exhaustion is **throttle**, not disconnect — this is the distinction that matters commercially. A throttled subscriber is restored at the next reset without operator action.
- Both are evaluated at auth **and** continuously by the enforcement worker, because a subscriber who crosses a limit mid-session must be acted on without waiting for a re-auth.

### 3.3 FR-130 — Unified exhaustion actions
One action set for all four limits: `block` · `throttle` (to a rate) · `switch_plan` (to another profile) · `expired_pool`.

**Evaluation order is frozen and tested** (§4). One documented order, one implementation, applied identically at auth time and in the worker — never two code paths that can drift.

### 3.4 FR-131 — Speed windows
- Time-of-day override with day-of-week control and midnight crossing.
- When several windows match, **highest speed wins**.
- A subscriber currently throttled by a limit is **excluded** from windows — a window must never undo an enforcement action.
- Resolved at auth into the `rate_limit` intent, and re-applied live by the worker on window entry and exit via CoA.

### 3.5 FR-132 — Plan ergonomics
Seed plans on fresh installs (tiers plus an "Expired" throttle plan) so a new install is usable before the operator has configured anything · one-click plan duplication · plan-level auto-renew with bulk propagation · plan list showing subscriber count, active limits, and windows at a glance.

### 3.6 Enforcement worker
Extends the existing time-driven sweep: evaluate limits, apply exhaustion actions, enter and exit speed windows, reset daily counters at the configured time, and enqueue CoA for anything affecting a live session. Idempotent — a re-run after a crash must not double-apply an action or re-charge anything.

## 4. Frozen contracts

**Evaluation order (frozen):**
1. Account status (disabled → reject)
2. NAS/service scoping
3. Expiry
4. Quota
5. Uptime
6. Daily
7. Speed windows (only if no exhaustion action is active)

**RADIUS reply contract amendment:** `rate_limit` may now be produced by a speed window or a throttle action, not only the profile rate. The intent itself is unchanged — vendor mapping stays confined to `internal/radius/vendor/`. `address_pool` behaviour is unchanged, including the rule that no pool intent is emitted when none applies.

**Counter contract:** `subscriber_counters` rows are resettable; lifetime totals live elsewhere (v4-5) and are **never** written by a reset.

**Worker contract:** every action is idempotent and audited; a CoA is enqueued, never sent inline from the evaluation path.

## 5. Acceptance gate

| # | Item | Proof |
|---|---|---|
| 1 | Four limits enforce at auth | Packet harness: one case per limit, accept and reject |
| 2 | Four limits enforce mid-session | Cross each limit while online; worker acts within one tick |
| 3 | Evaluation order | Table-driven test over conflicting limit combinations, asserting the frozen order |
| 4 | Daily throttle and restore | Cross the daily cap → throttled; advance past reset → restored, no operator action |
| 5 | Reset uses server timezone | Verified across a DST-free but non-UTC timezone (Asia/Baghdad) |
| 6 | Speed windows | Entry, exit, midnight crossing, overlapping windows (highest wins), day-of-week |
| 7 | Throttled excluded from windows | A throttled subscriber does not gain speed when a window opens |
| 8 | Each exhaust action | block / throttle / switch_plan / expired_pool each verified end-to-end |
| 9 | Worker idempotent | Kill mid-sweep, restart; no double-apply, no duplicate ledger or audit row |
| 10 | Seed plans only on empty | Fresh install seeds; existing install with profiles is untouched |
| 11 | Migrations linear | 0597–0600, applied cleanly on a restored realistic dataset |
| 12 | Existing behaviour unchanged | Every pre-v4-4 auth test passes unmodified |

## 6. Commit plan

1. `feat(profiles): uptime and daily limit fields, per-limit exhaust actions (FR-128, FR-129, 0597)`
2. `feat(subscribers): resettable counter table (FR-128, 0598)`
3. `feat(radius): unified exhaustion action set with frozen evaluation order (FR-130)`
4. `feat(profiles): speed windows (FR-131, 0599)`
5. `feat(radius): resolve speed windows into the rate_limit intent (FR-131)`
6. `feat(worker): live limit evaluation, window entry/exit, daily reset (FR-128–131)`
7. `feat(profiles): seed plans, duplication, auto-renew propagation (FR-132, 0600)`
8. `feat(panel): plan form and list for the four limits and windows (FR-128–132)`
9. `docs: v4-4 gate result`

## 7. Kickoff prompt

```
Execute phase v4-4 (Limits engine) of the HikRAD v4 program, solo and sequentially, start
to finish, in this repo.

Read first:
  - docs/v4/00-v4-master-plan.md
  - docs/v4/phases/phase-v4-4-limits-engine/00-phase.md   (this brief — §4 is frozen)
  - docs/prd/02-radius-nas-aaa.md and docs/prd/04-subscribers-profiles.md
  - backend/internal/radius/ (authorize path, AuthView, intents, vendor adapter)
  - backend/test/harness/ (the packet harness you will prove this with)

Deliver FR-128 through FR-132. Migrations 0597-0600 — the next free numbers; confirm 0595
is still the repo maximum before you write them.

This is the product's centre of gravity. HikRAD today enforces two limits (expiry, quota);
SAMM enforces four (expiry, quota, uptime, daily) plus speed windows, and that gap is the
most visible one in a side-by-side demo.

Get these right and later phases inherit correct behaviour:

1. ONE evaluation order, frozen in §4, ONE implementation, used identically at auth time
   and in the worker. Two code paths that can drift is the failure mode here.
2. Daily limit's default exhaustion is THROTTLE, not disconnect, and it self-restores at
   the next reset with no operator action. That distinction is the commercial point of the
   feature — do not model it as "quota with a shorter period".
3. A throttled subscriber is EXCLUDED from speed windows. A window must never undo an
   enforcement action (gate item 7).
4. Limits must act MID-SESSION, not only at re-auth. The worker enqueues CoA; it never
   sends inline from the evaluation path. v4-8 makes that queue durable — enqueue through
   the existing CoA path and do not pre-build v4-8's outbox here.
5. The worker must be idempotent. Kill it mid-sweep and restart: no double-apply, no
   duplicate ledger row, no duplicate audit row (gate item 9).

Vendor neutrality is unchanged: rate_limit stays an abstract intent, MikroTik mapping stays
inside internal/radius/vendor/, and the CI grep gate must stay green. The rule that no
address_pool intent is emitted when no pool applies is regression-locked — do not disturb it.

Every pre-existing auth test must pass UNMODIFIED (gate item 12). If one needs changing,
that is a signal you have altered behaviour that was not in scope — stop and re-read §2.

Finish with docs/history/gates/v4-4.md.
```
