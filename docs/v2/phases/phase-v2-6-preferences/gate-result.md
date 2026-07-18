# Phase v2-6 — Per-Manager Preferences & Subscriber Email — Integration Gate Result

Run date: 2026-07-18. Executed **solo + sequential** per the v2 execution
plan. The phase's `00-phase.md` (frozen contracts C1–C6) and its docs (PRD
Decision 39, sub-PRDs 01/04) had already been committed on 2026-07-18 during
an earlier session that stopped at kickoff Step 3, deferred twice (by v2-5
then v2-7 jumping ahead per Decisions 38/40) — this run picked the phase back
up, confirmed the frozen contracts still matched the current codebase (no
drift), verified migrations 0589/0590 were still the next free numbers, and
implemented everything through the gate in one pass.

Verification environment: throwaway **TimescaleDB (pg16) + Redis 7** standalone
containers (Postgres 5433, Redis 6380 on localhost), migrated fresh before the
full-suite run — same technique documented in `hikrad-dev-environment` /
prior phases' gate results.

`scripts/gate-v2-phase-6.sh` (pre-existing, written at the original kickoff):
**all scripted legs PASS, 0 FAIL.**

## Gate items 1–11

| # | Item | Result | Evidence |
|---|---|---|---|
| 1 | **Schema & migration** — `0589_manager_preferences`/`0590_subscribers_email` present, no `.down.sql`; `go build`/`go vet` clean | **PASS** | Both migration files present; forward-only confirmed by grep; `go build ./...` and `go vet ./...` clean; vendor-isolation grep (unaffected by this phase) green. |
| 2 | **No-row default is 200, not 404 (C1/C2)** | **PASS** | `TestPreferencesNoRowDefaultsToZeroValue` (`internal/auth/preferences_db_test.go`) — a fresh manager's `GET /me/preferences` returns `200` with every field at its zero value. |
| 3 | **Cross-device seed (AC-84a)** | **PASS** | `TestPreferencesCrossDeviceSeed` — manager A `PUT`s `{theme:"dark", language:"ku"}` in one login session, then a second independent login (no shared client state) `GET`s the same values back. |
| 4 | **Cross-manager isolation (AC-84b)** | **PASS** | `TestPreferencesCrossManagerIsolation` — manager A's `PUT` (including a spoofed `manager_id`/`id` field naming manager B) never appears in manager B's `GET`; A's own write sticks under A. The spoofed fields are simply unknown JSON keys the `Preferences` struct doesn't declare, so `encoding/json` drops them silently — the endpoint has no id parameter to even attempt redirecting the write, exactly as C2 specifies. |
| 5 | **Presentation-only boundary never crossed (FR-84.3)** | **PASS** | Two grep legs (mirroring v2-9's "no ancestry column" pattern): no code in `internal/radius`/`internal/billing` references `manager_preferences`/`notification_prefs`; `auth.Can`/`ScopeFilter` (`middleware.go`, `roles.go`) never reference `Preferences`. |
| 6 | **Validation (C3)** | **PASS** | `TestPreferencesValidationRejectsBadInput` — five subtests (bad theme/language/numerals/table_page_size, unknown notification key) each `422` with a `field_errors` entry naming the right JSON path (including the dotted `notification_prefs.typo_key` path); a final `GET` after all five rejected `PUT`s confirms nothing was written. |
| 7 | **Email validation (C4)** | **PASS** | `TestSubscriberEmailValidation` (`internal/subscribers/email_db_test.go`), 3 subtests: valid email persists and round-trips through the `GET /subscribers/{id}` detail composition; malformed email `422`s on create and writes zero rows; malformed email on update `422`s and leaves the existing value untouched. |
| 8 | **CSV import mapping (AC-85b, C5)** | **PASS** | `TestImportMapsEmailColumn` (`internal/importer/email_db_test.go`) — the `sas4` preset maps a header named `Email` (case-insensitively, confirmed via the upload response's `column_map`); dry-run flags the malformed-email row and reports `will_create: 1`; zero subscriber rows exist after dry-run; execute creates exactly the one valid row with `email` populated, the malformed row never created. |
| 9 | **Full regression** | **PASS** | Full `go test ./... -p 1` against the fresh DB/Redis pair: every package green except two already-documented pre-existing failures unrelated to this phase (see "Bugs found" below) — `internal/auth`, `internal/subscribers`, `internal/importer` all fully green, including every pre-v2-6 test in those three packages. |
| 10 | **Panel/portal** | **PASS** | `frontend/shared`/`panel`/`portal` builds clean; ESLint 0 errors (pre-existing unrelated fast-refresh warnings only) + Prettier clean; **43 shared + 70 panel + 14 portal vitest** all green; `i18n:check` green across en/ar/ku (0 missing keys, 0 hardcoded strings) covering the new "My preferences" screen, the subscriber form/list email field, and the portal settings email field. |
| 11 | **Docs accuracy** | **PASS** | `docs/PRD.md` carries FR-84/FR-85 and Decision 39 (committed in the original kickoff's Step-1 docs commit); sub-PRDs 01 and 04 carry FR-84/FR-85 respectively; phase brief (`00-phase.md`) present with all six frozen contracts (C1–C6) intact and unchanged from the original freeze — implementation matched the frozen shapes exactly, no amendment needed. |

## GREEN / RED verdict

**GREEN — 27/27 scripted legs** (`scripts/gate-v2-phase-6.sh`).

Human/hardware legs: **none** — this phase has no router/device/hardware
dependency (per the phase brief's own "Human/hardware legs: none"), matching
the posture of v2-4/v2-9/v2-5.

## Bugs found and fixed

**None new.** Two pre-existing, already-documented failures were re-confirmed
during the full-regression run (`internal/monitorsvc` `TestSubscriberEvents_RenewedDeliversWhatsAppReceipt`
and `internal/portalapi` `TestMockGatewayLifecycleReplayAndDisabled`/
`TestPortalCardPaymentSubmitAndQueue`) — both are dated 2026-07-17/2026-07-18
in `docs/ops/known-issues.md`, both unrelated to any file this phase touches
(`internal/auth`, `internal/subscribers`, `internal/importer`,
`internal/portalapi/me.go` only for a new field, `frontend/panel`,
`frontend/portal`), and both reproduce identically with or without this
phase's changes applied. No new row added to known-issues.md.

## Implementation notes

- **Contracts matched the freeze exactly.** `00-phase.md`'s C1 (`manager_preferences`
  schema), C2/C3 (`GET`/`PUT /me/preferences`), C4 (subscriber email column +
  validation), C5 (CSV `email` mapping key), and C6 (panel/portal client
  seeding) were all implemented as specified with no amendment — the docs
  freeze from the original kickoff needed zero changes.
- **jsonb marshaling follows the repo's existing convention** (`internal/importer/store.go`'s
  pattern): `manager_preferences.doc` is scanned as `[]byte` and
  `json.Unmarshal`ed into `Preferences`, not via a generic pgx struct scan —
  consistent with every other jsonb column in this codebase.
- **`SetEmail` (portal) intentionally carries no format validation**, mirroring
  the pre-existing `SetPhone`'s posture exactly — the phase brief's "same
  pattern as phone" instruction was read literally rather than as an
  opportunity to add validation `SetPhone` itself doesn't have. Panel/admin
  writes (`internal/subscribers/api.go`) *do* validate, per C4.
- **`landing_page`'s placeholder text (`/dashboard`) is deliberately identical
  across en/ar/ku** — it is a literal route path, not translatable prose,
  same class as `common.productName`; `i18n:check`'s "untranslated" warning
  list (not a failure) flags both for exactly this reason.
- **The panel's "My preferences" screen lives at both `/preferences` (routed,
  ungated) and a `UserMenu` dropdown item**, matching the existing `/account`
  self-service page's precedent of also being reachable from the admin nav
  group — the phase brief only required the `UserMenu` link, the route
  registration is required for that link to go anywhere.

## Test-harness notes (for whoever runs this next)

- Same traps prior phases documented: run `go test -p 1` against a **fresh**
  database. No new trap discovered this phase.
- The DB-gated detail-response shape trap: `GET /api/v1/subscribers/{id}`
  wraps the subscriber under `{"subscriber": {...}}` (the FR-3 composition
  contract), unlike `POST`/`PUT`'s top-level subscriber shape — a first draft
  of `TestSubscriberEmailValidation` decoded the wrapped GET response at the
  top level and got empty strings back before this was caught locally (never
  shipped, not a known-issues entry — caught before the gate run, not a
  production defect).
