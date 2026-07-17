# Phase v2-2 — NAS Auto-Setup Config Manager + Hotspot/PPPoE Server Management — Integration Gate Result

Run date: 2026-07-17. Executed **solo + sequential** per Decision 25 / the v2
execution plan, in six reviewable chunks (docs / schema+vendor-FR65-66 /
vendor-FR67 / API / backend tests / panel / gate).

Verification environment: a throwaway **TimescaleDB (pg16) + Redis 7** pair
brought up specifically for this phase's DB-gated legs, dropped and recreated
to a fresh schema before the final gate run (this repo's own documented
trap — a DB-gated legs suite proves nothing running against a database
another run already migrated).

`scripts/gate-v2-phase-2.sh`: **all 27 scripted legs PASS, 0 FAIL.**

## Gate items 1–12

| # | Item | Result | Evidence |
|---|---|---|---|
| 1 | **Migration** — `0520_nas_services_management_mode` adds the column with `DEFAULT 'router'` and a `CHECK` constraint; no `.down.sql`; number still free at implementation time (repo max was 0505) | **PASS** | File present; grep-verified default; forward-only per the repo-wide rule (Decision 25 amendment), not re-litigated by this phase. |
| 2 | **C1 non-invalidation** — the full existing `internal/radius/vendor` auto-setup suite passes unchanged with empty `Values`/`Resolutions` | **PASS** | Whole `mikrotik_autosetup_test.go` suite green; `PlanAutoSetup(conn, in, nil)` reproduces every pre-FR-66 expectation byte-for-byte (test call sites updated mechanically, assertions untouched). |
| 3 | **Config inspection (FR-65)** — `ReadConfig` returns the exact snapshot for planted state; 422/502 contract; audit row on success | **PASS** | `TestReadConfig_ReflectsPlantedRouterState`, `TestReadConfig_NeverExposesSecretValue`, `TestReadConfig_EmptyRouter_NoErrors` (fake-router); `TestNASConfig_NoCredentials_Returns422`, `TestNASConfig_HappyPath_ReturnsSnapshotAndAudits` (DB-gated HTTP). |
| 4 | **Form-driven values (FR-66.1)** — overrides change the plan and the copy-paste snippet identically | **PASS** | `autoSetupValuesInput.apply` is the single overlay function both `buildAutoSetupPlan` (auto-setup) and `configSnippetHandler` (snippet, via query params) call against the same `snippetInputFor` base — one function, two callers, cannot drift by construction rather than by a byte-comparison test. |
| 5 | **Modify-or-create (FR-66.2/66.3)** — update produces a `/set` never `/add`; keep drops cleanly; unresolved/abort/unknown match pre-FR-66; non-resolvable stays blocking even under "update"; hash covers values+resolutions+NAS id | **PASS** | `TestPlanAutoSetup_Resolution_Update_ProducesSetItem`, `..._Keep_DropsConflictKeepsOtherItems`, `..._UnresolvedOrAbort_MatchesPreFR66`, `..._NotResolvable_UpdateFallsThroughToConflict`, `..._IncomingAndPPPAAA_AreResolvable` (all three Resolvable conflict sources — `/radius`, `/radius/incoming`, `/ppp/aaa` — covered, not just the one from the phase-brief example); `TestPlanHash_DiffersAcrossValues/Resolutions/NAS` (pure-function). |
| 6 | **Never delete (FR-66.4/67.6)** — no `/remove` sentence anywhere in the auto-setup or service-provisioning files | **PASS** | Dedicated grep leg in the gate script; only hit is a comment sentence explaining the guarantee, not a literal. |
| 7 | **Vendor isolation (FR-17)** | **PASS** | `scripts/lint-vendor-isolation.sh` green; all of C2/C8's new RouterOS paths (`/interface/pppoe-server/server`, `/ip/hotspot`, `/ip/hotspot/profile`, `/ip/hotspot/user/profile`) live only in `internal/radius/vendor/mikrotik_config.go` and `mikrotik_service_provision.go`. |
| 8 | **Server create (FR-67.3)** — hotspot create plan includes the FR-62.7 guard on its OWN dedicated profile (never "default") from the first plan; conflicts are abort-only | **PASS** | `TestPlanService_CreateHotspot_IncludesFR627Guard` (asserts the guard sentence targets the new `<name>-hikrad` profile, not `default`), `TestPlanService_CreatePPPoE_Basic`, `TestPlanService_Create_NameCollision_Conflicts` (asserts `Resolvable: false`); DB-gated `TestServiceApply_CreateHotspot_PersistsSystemManaged` proves the HTTP round trip persists `management_mode='system'`. |
| 9 | **Adopt (FR-67.5)** | **PASS** | `TestServiceAdopt_FlipsModeWithZeroRouterWrites` — a fake `ROSConn` whose `Write` is never called (asserted via write count, not a mock expectation, so a future accidental write fails loudly); `TestServiceAdopt_RequiresConfirm`; `TestServiceAdopt_AlreadySystem_Returns409`. |
| 10 | **Edit-requires-adopt (FR-67.4)** | **PASS** | `TestPlanService_Edit_RequiresExistingMatch` (fake-router: editing a target the router no longer reports conflicts, never silently creates), `TestPlanService_Edit_ExistingMatch_ProducesSetNotAdd`, `TestServiceEdit_RouterManaged_RequiresAdopt` (DB-gated: 409 `not_adopted` with **zero** router I/O attempted — verified via the fake router's write counter). |
| 11 | **Panel** | **PASS** | `frontend/panel` build ✓, ESLint 0 errors (10 pre-existing unrelated warnings) ✓, prettier ✓, **70 vitest** (unchanged — this phase added no new component tests; see "Deviations") ✓, `i18n:check` OK across en/ar/ku with 0 missing keys and 0 hardcoded strings ✓. `frontend/shared` and `frontend/portal` workspaces build clean too. |
| 12 | **Docs accuracy** | **PASS** | Master PRD (FR-65–67, Decision 33) + sub-PRD 02 (elaboration, AC-65a–AC-67c, data/interfaces) + index (67/67 coverage) updated in the Step-1 commit, before any code. `known-issues.md`'s multi-hotspot row updated to describe the FR-66/FR-67 split (see below); two bugs found while building recorded with root cause. |

## GREEN / RED verdict

**GREEN — 12/12.**

## Bugs found and fixed (in `docs/ops/known-issues.md`)

**1. `serviceApplyHandler` panicked on a nil `*nasRegistry`.** `afterNASChange`
(shared by every NAS-mutating handler since Phase 4) calls
`m.nas.invalidate()`, and `autoSetupTestModule` — the DB-gated test harness
every `internal/radius` DB test since Phase 4 has built on — constructed a
bare `&module{db, log}` instead of mirroring `Register`'s actual wiring
(`module.go:38`, `m.nas = newNASRegistry(...)`). No test built through that
harness had ever exercised a handler reaching `afterNASChange` until this
phase's `serviceApplyHandler` did (`nas_api.go`'s own `createNASHandler`/
`updateNASHandler` DB tests, if any exist elsewhere, evidently don't use this
helper). Fixed at the harness: `autoSetupTestModule` now sets `m.nas` too, so
the test module's shape matches production's for good. Caught by actually
running the DB-gated legs rather than trusting them self-skipped — exactly
the discipline the harness self-skip convention exists to make optional but
not free of risk.

**2. `TestSubscriberEvents_RenewedDeliversWhatsAppReceipt` (unrelated package)
fails consistently** on this environment, found incidentally while gating
this phase (`internal/radius` itself was fully green in the same `-p 1` run).
Not diagnosed to a confirmed root cause — flagged with a plausible,
unconfirmed hypothesis (a fixed `150ms` sleep racing a Redis pub/sub
subscribe) rather than guessed at. Out of scope for this phase (NAS/RADIUS
domain, not billing/notifications); recorded for whoever next touches
`internal/monitorsvc`.

## Test-harness notes (for whoever runs this next)

- Same traps v2 phase 1 already documented: run `go test -p 1` against a
  **fresh** database. This phase adds one more: the DB-gated harness
  (`autoSetupTestModule` in `db_phase4_test.go`) now builds a NAS registry —
  if a future refactor of that helper drops it again, any handler calling
  `afterNASChange` will panic instead of failing cleanly. Consider asserting
  `m.nas != nil` in the helper itself if this recurs.
- Docker Desktop was not running at the start of this phase's DB-leg
  verification; a generic `postgres:16` pull stalled indefinitely on this
  network. This repo's own compose stack already has `timescale/timescaledb:
  latest-pg16` + `redis:7-alpine` cached from prior work — pulling those
  specific tags instead started in seconds. Worth remembering over reaching
  for an uncached image next time.

## Deviations from the brief

- **No new panel component test files.** The phase brief's gate item 11 asks
  for "vitest green," which the existing 70-test suite already satisfies
  (unchanged) — this phase's panel work (`NasAutoSetupModal` extensions,
  `ServiceProvisionModal`) is validated by TypeScript build + ESLint +
  `i18n:check` + manual reasoning about the API contracts it drives, not new
  RTL component tests. No existing NAS-page component test file was in the
  repo to extend as a pattern (`NasScopePicker.test.tsx` tests a different
  component), and adding a new one was judged lower-value than the backend
  DB-gated coverage for a UI whose real risk is the router write path, which
  the backend tests already lock. Flag if this should be revisited.
- **Router-config reuse.** FR-67.2/67.5's `GET .../services/{serviceId}/
  router-config` reuses `DiscoverServices` (FR-62.6) filtered to one instance,
  rather than a new vendor method reading exactly one object. The phase brief
  said "reuses FR-65's inspection plumbing"; `DiscoverServices` already
  returns precisely the fields this endpoint needs (interface, pool, enabled)
  and is already the established read-only-both-sides pattern for "show what
  the router really has" — a second near-identical vendor method would be the
  kind of premature parallel implementation the codebase's own conventions
  argue against.

## Human/hardware legs (documented-pending, per the phase brief)

Same sanctioned handling as every prior phase's device-dependent items — none
of these were exercised, per the phase brief's own explicit sanctioning:

1. **Create a real hotspot zone end-to-end** via FR-67.3 on a physical
   MikroTik and authenticate a real subscriber through it.
2. **Adopt a real pre-existing PPPoE server** via FR-67.5 and edit its default
   profile from HikRAD, confirming the router-side change via Winbox.
3. **Exercise an `update` resolution** (FR-66.2) against a real router's
   foreign `/radius` entry and confirm the rewrite via Winbox.
