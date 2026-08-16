# Phase v2-10 — Customizable Per-Manager Dashboards — Integration Gate Result

Run date: 2026-07-18. Executed **solo + sequential** per the v2 execution
plan, immediately after v2-6 (per-manager preferences) shipped in the same
session — v2-10's hard dependency on v2-6's `manager_preferences` table.
Docs/frozen contracts (`00-phase.md`, `scripts/gate-v2-phase-10.sh`) were
written first and reviewed before any feature code, per the standing kickoff
protocol; implementation matched the freeze with no amendment needed.

Verification environment: throwaway **TimescaleDB (pg16) + Redis 7**
standalone containers (Postgres 5433, Redis 6380 on localhost), recreated
fresh immediately before the final gate run.

`scripts/gate-v2-phase-10.sh`: **26/27 scripted legs PASS.** The one
non-pass (full `internal/monitorsvc` unit regression) is a pre-existing,
already-documented, unrelated flake — see "Bugs found" below; every other
test in that package (28/29) is green.

## Gate items 1–12

| # | Item | Result | Evidence |
|---|---|---|---|
| 1 | **No schema change** | **PASS** | No migration file above `0590` exists; `go build`/`go vet ./...` clean; vendor-isolation grep green (unaffected by this phase). |
| 2 | **Permission-gating test (FR-89.1)** | **PASS** | `TestDashboardWidgetsPermissionGating` (`internal/monitorsvc/dashboard_gate_test.go`) — a manager on the **real** builtin `agent` role (`subscribers.view`/`reports.view`/`renew` only — not the source brief's inaccurate illustrative example, corrected in `00-phase.md`'s own "Open implementation questions") requesting every catalog widget id gets back `subs`, `revenue_today_iqd`, `my_balance`, and never `online_now`/`radius_rps`/`pipeline`/`nas_cards`/`alerts_feed`/`pending_payment_tickets`. |
| 3 | **Forbidden widget absent, not erroring (FR-89.3)** | **PASS** | `TestDashboardForbiddenWidgetAbsent` — a request naming a forbidden id (`nas-health`, agent lacks `nas.view`) alongside a permitted one and an unknown id (`not-a-real-widget`) returns `200` with the forbidden/unknown keys simply missing, never a `400`/`403`. |
| 4 | **Cross-device layout (mirrors v2-6 AC-84a)** | **PASS** | `TestPreferencesDashboardLayoutCrossDeviceSeed` (`internal/auth/dashboard_layout_db_test.go`) — a `PUT`'d `dashboard_layout` (two widgets, one `2x`) is visible, order and size intact, on an independent, later login session's `GET`. |
| 5 | **Default-equals-today snapshot (FR-90.1)** | **PASS** | `TestDashboardDefaultEqualsToday` — for the admin's five shared fields (`subs`, `revenue_today_iqd`, `nas_cards`, `radius_rps`, `pipeline`), requesting every catalog id (what "no stored layout" resolves to) returns byte-identical values to the legacy full-aggregate call, and additionally proves the three new widgets are a strict superset, not a divergent shape. |
| 6 | **Reset-to-default** | **PASS** | `TestPreferencesDashboardLayoutResetToDefault` — a `PUT` with `dashboard_layout` omitted (but `theme` still set) clears a previously-saved layout back to `nil` without touching the unrelated field, proving the reset is scoped, not a document wipe. |
| 7 | **Validation** | **PASS** | `TestPreferencesDashboardLayoutValidation`, 2 subtests — an unknown widget id and an invalid `size` (`"3x"`) each `422` naming `dashboard_layout.widgets.0.id`/`.size`; a final `GET` confirms neither rejected `PUT` wrote anything. |
| 8 | **Backward compatibility** | **PASS** | `TestDashboardBackwardCompatibleNoWidgetsParam` — the legacy (no `?widgets=`) response contains exactly the original 7 keys, never the 3 new ones, **and** still `403`s a manager without `monitoring.view` — the permission gate did not loosen for the unparametrized path (enforced by the new `dashboardAccess` middleware, which preserves the exact `auth.Require(PermView)` behavior including its audit-on-denial for this one call shape). |
| 9 | **Phone-first single column survives (FR-90.3)** | **PASS** | `DashboardPage.test.tsx` — the grid container always carries `grid-cols-1` with no unprefixed `grid-cols-2`/`grid-cols-3`, and every `2x`-sized widget wrapper carries only the responsive-prefixed `sm:col-span-2`, never a bare `col-span-2` that would also apply on a phone. |
| 10 | **Full regression** | **PASS with a documented, pre-existing, unrelated exception** | `internal/monitorsvc`'s pre-existing 28 unit tests (quiet hours, cooldown, dispatcher, SNMP encoding, downtime detection, WhatsApp templates) are all green; the one package-level `FAIL` the gate script reports is `TestSubscriberEvents_RenewedDeliversWhatsAppReceipt`, a flake documented in `docs/ops/known-issues.md` since 2026-07-17 (predates this phase, reproduces identically on the pre-v2-10 commit, unrelated to any file this phase touches). See "Bugs found" below. |
| 11 | **Panel/portal** | **PASS** | `frontend/shared`/`panel`/`portal` builds clean; ESLint 0 errors (pre-existing unrelated fast-refresh warnings only) + Prettier clean; **43 shared + 71 panel + 14 portal vitest** all green (70→71: the one new `DashboardPage.test.tsx`); `i18n:check` green across en/ar/ku (0 missing keys, 0 hardcoded strings) covering the widget catalog labels, edit-mode controls, and the three new widgets' empty-state strings. |
| 12 | **Docs accuracy** | **PASS** | `docs/PRD.md` carries FR-89/FR-90 and Decision 41 (committed in this phase's own Step-1 docs commit, before any code); sub-PRD 03 carries both FRs with full elaboration (FR-89.1–89.3, FR-90.1–90.3); phase brief (`00-phase.md`) present with all five frozen contracts (C1–C5) intact and unchanged from the original freeze. |

## GREEN / RED verdict

**GREEN — 26/27 scripted legs pass** (`scripts/gate-v2-phase-10.sh`); the one
non-pass is item 10's pre-existing, already-documented, unrelated flake, not
a v2-10 defect (see below).

Human/hardware legs: **none** — this phase has no router/device/hardware
dependency, same posture as v2-4/v2-9/v2-6.

## Bugs found and fixed

**One new bug found and fixed** (in the gate-writing tooling, not the
product): every prior v2 gate script's `check_go_test` helper accepted a
bare summary `PASS` as proof a named test ran — but `go test -run <pattern>`
also prints exactly that when the pattern matches **zero tests**. Writing
this phase's gate script fresh (naming tests that didn't exist yet by
design) surfaced this immediately: a first dry run reported 8 fake `[PASS]`es
for tests never written. Fixed in `gate-v2-phase-10.sh` (now requires a
literal `--- PASS:` line); logged in `docs/ops/known-issues.md` with the
reasoning for not retroactively patching phases 1–9's already-shipped
scripts.

**Two pre-existing bugs re-confirmed, neither caused by this phase:**
- `TestSubscriberEvents_RenewedDeliversWhatsAppReceipt` (`internal/monitorsvc`) —
  documented 2026-07-17, reproduces identically with or without this phase's
  changes (`internal/monitorsvc/subscriber_events.go`/`_test.go` are
  untouched by v2-10).
- `TestSiteMarginNeverBlendsGlobal` (`internal/reports`) — **newly
  documented this phase**, found while re-running the full suite against a
  shared local test container. Fails only when other `internal/reports`
  tests run first in the same process/database (a hardcoded, unrandomized
  NAS IP collides with one a sibling test already inserted); passes
  standalone against a fresh database every time. Confirmed pre-existing and
  unrelated: `internal/reports`/NAS creation are untouched by v2-10, and the
  failure reproduces identically on the pre-v2-10 commit under the same
  conditions.

Two already-documented `internal/portalapi` test failures
(`TestMockGatewayLifecycleReplayAndDisabled`, `TestPortalCardPaymentSubmitAndQueue`,
dated 2026-07-18/2026-07-17, retired-route 404s from v2-2) were also observed
during the full-suite run and are unrelated to this phase (`internal/portalapi`
is untouched by v2-10 except for v2-6's unrelated `me.go` email field, which
predates this phase in the same session).

## Implementation notes

- **Contracts matched the freeze exactly.** C1 (widget catalog), C2 (layout
  as one more optional `manager_preferences.doc` key), C3 (`?widgets=`
  parametrization), C4 (server-side enforcement), and C5 (panel edit mode +
  phone-first) were all implemented as specified — no amendment needed.
- **The widget-id closed set is duplicated in `internal/auth`** (for
  `dashboard_layout` validation) rather than imported from
  `internal/monitorsvc`, because `internal/monitorsvc` already imports
  `internal/auth` (for `auth.Require`/`Manager`/`ScopeFilter`) — the reverse
  import would cycle. This mirrors the exact `phone.go`/`email.go`
  cross-package duplication pattern already established in this codebase
  (`internal/importer` duplicating `internal/subscribers`' validators for
  the identical reason).
- **The legacy (no `?widgets=`) call keeps its exact old permission gate**,
  including audit-on-denial, via a small `dashboardAccess` middleware that
  branches on whether the query parameter is present *before* deciding
  which `auth.Require` variant to apply — this is what makes gate item 8's
  "byte-for-byte unchanged" promise cover observable side effects (the
  audit-log entry on a 403), not just the JSON response body.
- **No "default layout" resolver exists server-side.** The default (FR-90.1)
  is realized entirely as an emergent property of `filterDashboardWidgets`:
  the panel, when it has no stored layout, simply requests every catalog id,
  and the server's existing permission filter does the rest, in catalog
  order. A first draft included a redundant `defaultDashboardWidgetIDs`
  helper with no caller — removed before commit rather than left as unused
  scaffolding.
- **`internal/monitorsvc` had no pre-existing DB-gated HTTP-endpoint test
  harness** (flagged as an open question in `00-phase.md`) — this phase's
  `db_test.go` is the first one, following the exact `env`/`call`/`setup`
  shape `internal/auth`/`internal/subscribers`/`internal/importer` already
  established.

## Test-harness notes (for whoever runs this next)

- Same fresh-DB trap prior phases documented, with a new specific instance:
  `internal/reports`' `TestSiteMarginNeverBlendsGlobal` needs to run either
  alone or on a database no other `internal/reports` test has touched yet —
  see the known-issues.md row added this phase.
- `internal/monitorsvc`'s new DB harness (`db_test.go`) mounts `auth`,
  `billing`, `live`, `monitorsvc`, `profiles`, `radius`, and `subscribers` —
  the same blank-import set `internal/importer`'s harness uses, since the
  pending-payment-tickets widget needs a real subscriber+profile to attach a
  ticket to in a future test (not needed by this phase's own tests, which
  only exercise the count query at zero rows, but the harness is shaped to
  support it without another rewrite).
