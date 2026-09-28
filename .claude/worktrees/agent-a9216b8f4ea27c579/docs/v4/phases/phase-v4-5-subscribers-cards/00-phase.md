# Phase v4-5 — Subscribers, counters & card groups

> **FRs:** FR-133 … FR-138 · **Migrations:** 0601–0605 · **Depends on:** v4-1, v4-4 · **Blocks:** v4-11, v4-12
> **Owner sub-PRD:** 04 (subscribers) + 05 (cards/vouchers) · **Decisions committed at kickoff:** 65

## 1. Intent

Three gaps, all of which hotspot operators hit daily:

- **No batch creation.** Creating 200 numbered accounts is 200 form submissions. SAMM does it in one pass. This is a workflow blocker, not a nicety.
- **Counters conflate two different things.** HikRAD's usage figures serve both "how much has this subscriber consumed this period" (resettable, drives limits) and "how much has this subscriber ever consumed" (lifetime, drives billing disputes). Resetting a quota currently destroys history. SAMM keeps them separate, and it must be a data-model split, not a UI presentation.
- **Vouchers are flat.** One code, one profile, one use. SAMM's card *groups* carry their own speed, transfer, uptime and expiry limits independent of any plan, plus a shelf date, add-more, duplicate-group, and per-card resets. Prepaid hotspot resale is built on that model.

## 2. Scope

**In:** batch creation, the counter split, subscriber-list depth, card groups replacing flat vouchers, card printing, legacy importers.
**Not in:** the portal's view of any of this (v4-11), reports (v4-12), card designer (a later candidate).

## 3. Work

### 3.1 FR-134 — Counter split (do this first; everything else depends on it)
- **0601** — `subscriber_billing_counters`: lifetime bytes down/up, lifetime session seconds, first-seen, last-seen. **Never** written by a reset; only ever incremented by the accounting consumer.
- Backfill from existing usage history so no install loses its lifetime totals — a gate item, verified by comparing pre- and post-migration sums.
- `subscriber_counters` (created in v4-4, migration 0598) stays the resettable side. Every reset action touches only that table, enforced by a test and by DB-level permissions where practical.
- The subscriber page shows both, labelled distinctly. An operator must never wonder which number they are looking at.

### 3.2 FR-133 — Batch / numbered creation
`POST /api/v1/subscribers/batch`: prefix, start index, count, digit padding, shared or generated passwords, plan, expiry, auto-renew, owner, service type, and a per-subscriber invoicing choice (invoice each / invoice none / one invoice for the batch).

- Dry-run first, returning the exact usernames that will be created and any collisions.
- **Per-row independence** — one collision does not roll back the rest, matching the CSV importer's contract.
- Subject to the licence subscriber cap (FR-124): a batch that would cross the wall is refused **before** creating anything, not partway through.
- Audited as one batch event plus per-row rows.

### 3.3 FR-135 — Subscriber list depth
Per-limit progress bars (daily bar, quota / uptime / days remaining), the **plan-swap badge** showing current plan and, when a temporary throttle plan is in force, the arrow-joined pair, plus the online / active / suspended / expired filter set. Filters persist per manager via the existing `usePersistentState`.

### 3.4 FR-136 — Card groups (Decision 65, migrations 0602–0604)
- **0602** — `card_groups`: name, description, username prefix, code format (numeric/alpha/alphanumeric), code length, count, price, and the group's own limits — download/upload rate, transfer limit (unlimited / combined / separate down+up), uptime cap, expiry-after-first-login, and a `valid_until` shelf date.
- **0603** — `cards`: group id, code, state (`unused` → `active` → `exhausted` | `disabled`), first-login time, per-card counters.
- **0604** — **migrate existing vouchers into synthetic groups**, one group per existing batch, preserving every code byte-for-byte. No operator's printed vouchers stop working; a gate item.

Group actions: enable/disable all · extend `valid_until` · **add more cards to an existing batch** · reset an individual card's counters · **duplicate group** (same settings, fresh codes — the one-click reorder that operators actually use).

Auth-time: a card is a credential resolving to its group's limits, evaluated through **the same** v4-4 limit engine as a subscriber — not a parallel implementation. Expiry counts from first login; `valid_until` is a hard shelf date checked independently.

### 3.5 FR-137 — Printing and tracking
Branded PDF batches (logo from the identity resolver, auto-sized), per-card usage view, and card traffic tracked distinctly so v4-12 can report on Users and Cards separately.

### 3.6 FR-138 — Legacy importers (0605)
Radius Manager / Super Speed Radius MySQL dumps and Pro Radius XLSX, sharing the existing FR-6 wizard and its entity-parameterised batch tables (0605 adds the source-system column). Imports **plans and subscribers only** — never billing history, invoices, cards, tickets or accounting, which do not map cleanly. Preview with warnings, duplicate-username handling choice, per-row independence, and a post-import prompt to review imported plan prices before invoicing.

## 4. Frozen contracts

- `POST /api/v1/subscribers/batch` and its dry-run shape.
- Billing counters are **append-only in effect**: no reset path may write them. Enforced by test.
- Card codes are immutable after generation; group settings changes never alter existing codes.
- Cards evaluate through the v4-4 limit engine — no second enforcement implementation.
- Importers write only plans and subscribers.

## 5. Acceptance gate

| # | Item | Proof |
|---|---|---|
| 1 | Counter split | Reset quota; lifetime totals unchanged; both shown distinctly on the subscriber page |
| 2 | Backfill lossless | Sum of lifetime bytes before migration == after, on a restored realistic dataset |
| 3 | Batch creation | 200 accounts in one pass; dry-run matches the result exactly |
| 4 | Batch collision handling | Seed a collision; that row fails, the rest succeed |
| 5 | Batch respects licence cap | A batch crossing cap + buffer is refused before creating anything |
| 6 | Card group lifecycle | unused → active → exhausted by each of quota, uptime, and expiry-from-first-login |
| 7 | Shelf date independent | A card inside its post-login window but past `valid_until` is refused |
| 8 | Voucher migration lossless | Every pre-existing code still redeems; count and codes identical pre/post |
| 9 | Group actions | enable/disable all, extend, add-more, per-card reset, duplicate-with-fresh-codes |
| 10 | Cards use the v4-4 engine | Grep confirms no second limit implementation; harness verifies a card hitting each limit |
| 11 | PDF branded | Batch PDF carries the instance logo via the identity resolver |
| 12 | Importers | One dump per legacy system imported; plans and subscribers only; per-row independence |
| 13 | Migrations linear | 0601–0605 clean on a restored dataset |

## 6. Commit plan

1. `feat(subscribers): split lifetime billing counters from resettable counters (FR-134, 0601)`
2. `feat(subscribers): batch/numbered creation with dry-run (FR-133)`
3. `feat(panel): subscriber list progress bars, plan-swap badge, status filters (FR-135)`
4. `feat(cards): card groups with own limits and shelf date (FR-136, 0602-0603)`
5. `feat(cards): migrate existing vouchers into synthetic groups (FR-136, 0604, Decision 65)`
6. `feat(cards): group actions — enable-all, extend, add-more, duplicate, per-card reset (FR-136)`
7. `feat(radius): cards evaluate through the v4-4 limit engine (FR-136)`
8. `feat(cards): branded PDF batches and per-card usage (FR-137)`
9. `feat(import): Radius Manager, Super Speed Radius, Pro Radius importers (FR-138, 0605)`
10. `docs: v4-5 gate result`

## 7. Kickoff prompt

```
Execute phase v4-5 (Subscribers, counters & card groups) of the HikRAD v4 program, solo
and sequentially, start to finish, in this repo.

Read first:
  - docs/v4/00-v4-master-plan.md            (Decision 65)
  - docs/v4/phases/phase-v4-5-subscribers-cards/00-phase.md   (this brief)
  - docs/v4/phases/phase-v4-4-limits-engine/00-phase.md       (the limit engine you reuse)
  - docs/prd/04-subscribers-profiles.md and docs/prd/05-billing-payments-vouchers.md
  - backend/internal/subscribers/bulk.go and the FR-6 import wizard

Deliver FR-133 through FR-138. Migrations 0601-0605; confirm the repo maximum first.

Do FR-134 (the counter split) FIRST — everything else in this phase depends on the tables
it creates, and the backfill is the riskiest step. Today a quota reset destroys history
because one set of counters serves both limit enforcement and lifetime billing totals.
Split them at the data model, not in the UI, and prove the backfill is lossless against a
restored realistic dataset (gate item 2).

Then, in order: batch creation, list depth, card groups, printing, importers.

Binding rules:
  - Cards evaluate through the v4-4 limit engine. Do NOT write a second enforcement
    implementation for cards — gate item 10 greps for exactly that.
  - Existing vouchers migrate into synthetic groups with codes preserved BYTE-FOR-BYTE.
    Operators have printed these. Gate item 8.
  - Batch creation is per-row independent (one collision does not roll back the rest) and
    is refused up front if it would cross the licence cap + buffer — never partway through.
  - No reset path may ever write the lifetime billing counters. Enforce it with a test.
  - Importers write plans and subscribers only. Not billing history, invoices, cards,
    tickets, or accounting — those do not map and importing them creates false books.

Reuse rather than fork: the batch endpoint should structurally mirror subscribers/bulk.go,
and the legacy importers should extend the entity-parameterised FR-6 wizard rather than
becoming a fourth import path.

Finish with docs/history/gates/v4-5.md.
```
