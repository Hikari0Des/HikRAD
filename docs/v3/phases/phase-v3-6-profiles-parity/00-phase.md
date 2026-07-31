# Phase v3-6 — Profiles Management Parity

> Kicked off 2026-07-23 (PRD Decision 52). SOLO, sequential. Owned by **sub-PRD 04** (subscribers/profiles). Migration **0594**. FR-111. No frozen-contract amendment — this phase adds new endpoints and generalizes an existing one; the subscriber bulk and subscriber import contracts are untouched.

## 1. Problem (grounded)

The Subscribers page has had three management affordances since v1 that the Profiles page never got:

- **Filter bar** — `UserListPage.tsx` has a per-manager-remembered (`usePersistentState`) filter row with removable chips + "clear filters". `ProfilesPage.tsx` renders a bare table with no filtering at all — an ISP with dozens of plans (per site × per speed × per currency) scrolls to find one.
- **Bulk actions** — subscribers have `POST /api/v1/subscribers/bulk` (`bulk.go`) + `BulkBar.tsx`: selection-replaces-filter, async job, per-row failures. Profiles have only one-row Edit/Archive/Delete buttons; archiving last quarter's plans is a click-per-plan slog.
- **CSV import** — `internal/importer` + `ImportWizardPage.tsx` import subscribers from a SAS4 CSV. There is no way to bulk-load a plan catalog; every plan is hand-entered.

## 2. Scope

FR-111 only. No online dependency, no RADIUS-contract change, no router interaction. Three deliverables — a filter bar, a bulk endpoint+bar, and profile CSV import — plus the migration that makes the shared import tables entity-aware.

**Out of scope:** any change to the subscriber bulk/import contracts or their responses (the importer is *generalized*, never forked); a uniqueness constraint on `profiles.name` (a behavior change to an endpoint this FR doesn't own — see the duplicate-handling note in C3); the v3-1 restyle of these screens (this ships management parity on today's design system, which v3-1 will later overhaul in place).

## Migration budget
**0594** only (`import_batches.entity`). Repo max becomes 0594.

## Frozen contracts

### C1. Filter bar (FR-111.1) — panel only, no API change
- `ProfilesPage.tsx` gains a filter row above the table: text query (matches plan name, case-insensitive), archived (`any`/`no`/`yes`, default `no` to match today's default list which shows archived greyed but is dominated by active), quota mode (`any` + the three modes), expiry behavior (`any`/`block`/`expired_pool`), pool (`any` + the known pools). Remembered per manager via `usePersistentState('profiles.filters', EMPTY)` — the exact mechanism the subscriber/live/audit lists use.
- Filtering is **client-side over the already-loaded list** (`listProfiles(true)` returns everything; profiles are unpaginated for the same reason the subscriber list filters client-side). Removable active-filter chips + a "clear filters" link, same components/shape as `UserListPage`.
- **Today's list currently loads `listProfiles(true)`** (archived included) and greys archived rows; that stays. The default `archived: 'no'` filter simply hides them by default, with `any`/`yes` to bring them back — no data-fetch change.

### C2. Bulk endpoint + bar (FR-111.2)
- **`internal/profiles/bulk.go`** (new): `POST /api/v1/profiles/bulk` + `GET /api/v1/profiles/bulk/{id}`, structurally mirroring `subscribers/bulk.go` — the in-memory `jobRegistry`/`bulkJob`/`snapshot` pattern (copy, don't share: the packages don't cross-import), 202 + job snapshot, 700 ms-pollable status, per-row `bulkFailure{profile_id, name, error}`.
- **Request**: `{filter: {q, archived, quota_mode, expiry_behavior, pool_id}, profile_ids: []string, action, params}`. A non-empty `profile_ids` is an explicit selection that **replaces** the filter (identical semantics to the subscriber endpoint's `subscriber_ids`).
- **Actions + params**:
  - `archive` / `unarchive` — set `archived` true/false.
  - `change_pool` — `params.pool_id` (uuid or `""`/null to clear); validated to an existing pool or empty.
  - `set_address_list` — `params.address_list` (string; `""` clears → NULL).
  - `set_expiry_behavior` — `params.expiry_behavior` ∈ `block|expired_pool`.
  - `set_quota_behavior` — `params.quota_behavior` ∈ `block|expired_pool` **only**; `throttle` is refused `422` up front (it requires a per-plan `throttle_rate` the single-row validator enforces and a bulk verb can't supply).
  - `delete` — per row: re-run `profileInUse`; refuse an in-use plan as a per-row failure naming archive; otherwise `DELETE`.
  - `export` — synchronous CSV (name, price, currency, duration_days, rate_down_kbps, rate_up_kbps, quota_mode, expiry_behavior, quota_behavior, pool, address_list, archived), `export` permission.
- **Permission per action** (`bulkActionPerm`): everything `profiles.edit`; `delete` → `profiles.delete`; `export` → `export`. Enforced before any work — a bulk verb never weakens its single-row permission.
- **Every mutated row**: one audit entry (`profile.bulk_<action>`, actor captured for the detached goroutine via `auth.AuditActor`) **and** `radius.InvalidatePolicyByProfile(subscriberIDsOnProfile)` — a plan edit that skipped invalidation would serve stale policy for up to the cache TTL. (Reuse `subscribersOnProfile`.)
- **No manager scoping.** No `auth.ScopeFilter` — profiles are instance-global and no profile endpoint has ever been scoped. `selectTargets` runs the filter (or the explicit id list) with no owner clause. This is the one deliberate divergence from `subscribers/bulk.go`, and it is called out in a code comment so a future reader doesn't "fix" it into a bug.
- **`ProfileBulkBar.tsx`** (new, panel): mirrors `BulkBar.tsx` — scope line ("N selected" vs "every matching plan"), action buttons gated by `can(...)`, a param prompt modal (pool picker / address-list text / behavior select / delete warning), job progress bar + per-row failure list. Toggle from a "Bulk" button in the Profiles page header; row checkboxes + select-all-shown appear only in bulk mode (exactly `UserListPage`'s pattern).

### C3. Entity-parameterized CSV import (FR-111.3)
- **Migration 0594**: `ALTER TABLE import_batches ADD COLUMN entity text NOT NULL DEFAULT 'subscribers' CHECK (entity IN ('subscribers','profiles'))`. The default backfills every existing batch as `subscribers` — the only entity that existed — so nothing about the subscriber import changes.
- **`internal/importer` generalization** (never a fork):
  - `entity` threaded through `batch`/`batchColumns`/`scanBatch`/`insertBatch`; upload captures it from the route.
  - Routes: keep `POST /api/v1/import/subscribers`; add `POST /api/v1/import/profiles` (permission `profiles.create`; upload/map/dry-run/execute gated `profiles.view`/`profiles.create` the way the subscriber routes are gated `subscribers.view`/`subscribers.create`). The `{batch}` map/dry-run/execute/status routes are shared — they read the batch's stored `entity` and branch.
  - **Field catalog + preset per entity** (`preset.go`): a `profiles` field set (`name`* required, `price`, `currency`, `duration_days`, `rate_down`, `rate_up`, `session_limit`, `quota_mode`, `quota_total_bytes`, `expiry_behavior`, `quota_behavior`, `address_list`) and a `sas4-profiles`-style best-effort preset. `hikradFields` becomes `fieldsFor(entity)`; the `username`-required check becomes `requiredField(entity)` (`username` for subscribers, `name` for profiles).
  - **Dry-run validation** (`validate.go`): branch on entity. Profile rows validate: name present; price/duration/rates/session_limit numeric (rates via the shared prefix parse — a Go `parseRateKbps` mirroring `@hikrad/shared` binary multiples, added to the importer or reused from `radius` if one exists); quota_mode ∈ enum; expiry/quota behavior ∈ enum; **duplicate name within the file** and **name already in the live `profiles` table** → `skip` (the structural difference from subscribers: `profiles.name` is not unique, so there is no create-time 409 to lean on). The FR-105 license-cap heads-up block stays subscribers-only.
  - **Execute** (`execute.go`): self-dispatch to `POST /api/v1/subscribers` **or** `POST /api/v1/profiles` by entity, via the same in-process router mechanism, forwarding the bearer token, same idempotency posture. For profiles, a row whose name already exists is skipped (checked immediately before dispatch, since the create endpoint won't 409). `createBody` becomes an entity-shaped body builder.
- **Panel**: `ImportWizardPage` becomes entity-aware (an `entity` prop / route param). `/import` stays the subscriber wizard; add `/import/profiles` (guard `profiles.create`) + a Profiles-page "Import CSV" entry. Field lists, preset guess, and dispatched endpoint come from the entity. `api/importer.ts` gains an `entity` argument on upload; every other call already keys off `batch_id`.

## Integration gate
1. **C1 filter bar** — a panel test drives the Profiles filter row: a name query + archived filter narrow the list, chips render + remove, "clear" restores, and the selection persists across a remount (`usePersistentState`).
2. **C2 bulk endpoint** — Go tests (DB-backed, self-skip without `HIKRAD_TEST_DB_URL`): selection-replaces-filter; `archive`/`change_pool`/`set_address_list` mutate + audit + (assert) enqueue invalidation; `set_quota_behavior:throttle` → 422 with no write; `delete` over a selection with one in-use plan → that row fails naming archive, others delete; a `profiles.edit`-only actor calling `delete` → 403, nothing touched. A panel test renders `ProfileBulkBar` scope line for selected-vs-filter.
3. **C3 import** — Go tests: `POST /api/v1/import/profiles` upload→map→dry-run→execute round-trips a small plan CSV; a duplicate-name row is a dry-run skip and execute creates the rest; re-execute creates nothing. `POST /api/v1/import/subscribers` and its dry-run/execute responses are **byte-unchanged** (a regression test asserts the subscriber path still behaves exactly as before — the entity default is `subscribers`).
4. **v2-12 CI floors** green: `lint-no-native-controls.sh` (both new panel surfaces use the form control set), `layoutSmoke`, stylelint logical-properties, `i18n:check` (every new string trilingual en/ar/ku). New audit actions get trilingual `auditlog.action.*` titles (the v2.x maintenance pattern).
5. **Backend** `go build`/`vet`/`test` green; **panel** `tsc --noEmit`/lint/tests green; harness smoke unaffected.
6. **No regressions**: existing importer, profiles, subscribers, and radius tests pass unchanged.

## Owner decisions (Decision 52, confirmed)
Profiles get the subscriber page's management parity (owner item #5). Import is generalized, not duplicated. No manager scoping on profile bulk (profiles are instance-global). No `profiles.name` uniqueness constraint (out of FR scope).

## Documented-pending
None expected beyond the standing, unrelated `cmd/hikrad-updaterd` rollback-test failures (confirmed pre-existing during v3-3; that package depends on nothing this phase touches).
