# Phase v2-11 — Instance Branding — Integration Gate Result

Run date: 2026-07-18. Executed **solo + sequential** per the v2 execution
plan. Docs/frozen contracts (`00-phase.md`, `scripts/gate-v2-phase-11.sh`)
were written and reviewed before any feature code, per the standing kickoff
protocol; the owner then clarified mid-review (Decision 43, still pre-code)
that a fixed, non-configurable HikRAD attribution must survive full customer
rebranding — `00-phase.md` was amended in place (contract C10) before
implementation started, exactly as the protocol requires.

Verification environment: throwaway **TimescaleDB (pg16) + Redis 7**
standalone containers (Postgres 15432, Redis 16379 on localhost), started
fresh for this phase's gate run.

`scripts/gate-v2-phase-11.sh`: **39/40 scripted legs PASS.** The one
non-pass (`internal/portalapi` full-package regression) is two
pre-existing, already-documented, unrelated test failures — see "Bugs
found" below; every other test in that package, including this phase's own
three new ones, is green.

## Gate items 1–17

| # | Item | Result | Evidence |
|---|---|---|---|
| 1 | **No schema change** | **PASS** | No migration file above `0590` exists; `go build`/`go vet ./...` clean; vendor-isolation grep green (unaffected by this phase). |
| 2 | **Bug 1/2 fixed, public endpoint (DB-gated)** | **PASS** | `TestBrandingEndpointReflectsConfiguredIdentity` (`internal/portalapi/branding_db_test.go`) — `PUT /api/v1/settings/branding` then an unauthenticated `GET /api/v1/branding` returns the configured name/colors, including `background_color` (a fourth, smaller bug this consolidation closed as a side effect — it was never populated at all pre-phase). |
| 3 | **Bug 1/2 fixed, Hotspot package (DB-gated)** | **PASS** | `TestHotspotPackageEmbedsConfiguredBranding` (`internal/radius/hotspot_branding_db_test.go`) — the generated `login.html` contains the configured name and a base64 `data:` URI logo, and never references `/api/v1/branding` (stays self-contained, NFR-7). |
| 4 | **Bug 3 fixed, receipts (DB-gated)** | **PASS** | `TestReceiptBrandingBooleanRespected` (`internal/billing/receipt_branding_db_test.go`) — `receipt_branding=true` shows the configured name; `=false` shows the generic literal regardless of what's configured. |
| 5 | **Logo upload validation** | **PASS** | `TestStoreLogoValidation` (`internal/platform/branding_test.go`, pure filesystem, no DB needed) — an oversized file, an `.exe` disguised as `.png`, and a script-bearing SVG each rejected; a valid PNG and a valid SVG each round-trip byte-identical via `ReadLogoBytes`, including the single-current-asset replacement (SVG upload clears the prior PNG). An oversized-dimension raster is also rejected. |
| 6 | **Logo removal falls back cleanly** | **PASS** | `TestDeleteLogoFallsBackCleanly` — `DeleteLogo` clears the stored file; `ReadLogoBytes` reports none present; deleting again is not an error. |
| 7 | **`logo_url` rejected on the generic group PUT (DB-gated)** | **PASS** | `TestBrandingGroupPutRejectsLogoURL` (`internal/platform/setupapi/branding_logo_db_test.go`) — `PUT /api/v1/settings/branding` with `logo_url` in the body `422`s naming that field; `name`/colors still work through the same endpoint. |
| 8 | **Audit (DB-gated)** | **PASS** | `TestBrandingChangesAudited` — a name change and a logo upload each produce a `settings.update` `audit_log` row against `entity_id='branding'`. |
| 9 | **Panel threading** | **PASS** | `branding.test.tsx` — the sidebar and the panel's own login screen (which showed neither name nor logo before this phase) render the configured name from a mocked `GET /api/v1/branding`, and `document.title` updates once it resolves. |
| 10 | **Offline / no external fetch (grep)** | **PASS** | `internal/platform/branding.go`, the Hotspot builder, and the receipt renderer contain no `http.Get`/`http.Post`/`http.NewRequest`/`http.Client{` — pure disk/DB reads. |
| 11 | **Portal + PWA regression** | **PASS** | The portal's existing branded-login/RTL suite (`src/test/portal.test.tsx`) stays green; new `BrandedManifestLink.test.tsx` in **both** apps (neither existed pre-phase) proves the manifest `<link>`/theme-color `<meta>` swap to real configured data — the panel's version already did its own fetch, the portal's already read the shared context; both were correct on the consuming side and needed no code change, only the endpoint fix (C7). |
| 12 | **Panel/portal build/lint/i18n** | **PASS** | `frontend/shared`/`panel`/`portal` build clean; ESLint 0 errors (pre-existing unrelated fast-refresh warnings only) + Prettier clean; **75 panel + 16 portal vitest** all green (71→75 panel: `branding.test.tsx`, `shell/PoweredByFooter.test.tsx`, `pwa/BrandedManifestLink.test.tsx`; 14→16 portal: `shell/PoweredByFooter.test.tsx`, `pwa/BrandedManifestLink.test.tsx`); `i18n:check` green across en/ar/ku (0 hardcoded strings, 0 missing keys; the new `common.poweredBy` key translated in all three). |
| 13 | **Full regression** | **PASS with a documented, pre-existing, unrelated exception** | `internal/platform`, `internal/radius`, `internal/billing` full suites green. `internal/portalapi` reports `FAIL` at the package level from two already-documented 2026-07-18 test failures (`TestMockGatewayLifecycleReplayAndDisabled`, `TestPortalCardPaymentSubmitAndQueue`) unrelated to this phase — see "Bugs found" below. This phase's own three new `internal/portalapi`/`internal/radius`/`internal/billing`/`internal/platform` tests are all green within that same run. |
| 14 | **Fixed attribution present, everywhere it should be (FR-93.1)** | **PASS** | `shell/PoweredByFooter.test.tsx` in both apps — the mark renders on the authenticated shell **and** the login screen even when a fully configured custom identity (name "Nur Net", logo, colors) is present, proving the two coexist. |
| 15 | **Fixed attribution is structurally non-configurable (FR-93.2, grep)** | **PASS** | Neither `PoweredByFooter.tsx` contains a `useBranding(`/`fetch(`/branding-module import (checked against real code patterns, not the bare word "branding" — the components' own doc comments legitimately mention it in prose explaining what they deliberately don't do); no settings-group field name matches `*attribution*`/`*powered_by*`/`*footer*` anywhere in `settings_api.go`. |
| 16 | **Fixed attribution absent from print surfaces (FR-93.3)** | **PASS** | `receipt.go`, `hotspot.go`, and `PrintHeader.tsx` each contain no literal `"Powered by"` string. |
| 17 | **Docs accuracy** | **PASS** | `docs/PRD.md` carries FR-91/FR-92/FR-93 and Decisions 42/43; sub-PRD 01 carries FR-91, sub-PRD 07 carries FR-92/FR-93, sub-PRD 08 references the branding endpoint; `docs/ops/known-issues.md`'s branding row and the new `enforceJSON` row (see below) are both present, updated to Fixed in the same commit as the code that fixes them. |

## GREEN / RED verdict

**GREEN — 39/40 scripted legs pass** (`scripts/gate-v2-phase-11.sh`); the one
non-pass is item 13's pre-existing, already-documented, unrelated
`internal/portalapi` flake, not a v2-11 defect (see below).

Human/hardware legs: **none** — no router/device/hardware dependency (the
Hotspot package is generated and inspected as a file, never uploaded to a
live router by this phase's own gate), same posture as v2-4/v2-6/v2-9/v2-10.

## Bugs found and fixed

**Three pre-existing bugs, found during kickoff research (before any code),
all fixed by this phase** — full root-cause detail already recorded in
`docs/ops/known-issues.md` and the phase brief's Problem section:
1. The public `GET /api/v1/branding` endpoint and the Hotspot captive-portal
   page both read a settings key, `"branding"`, that has never existed.
2. Their struct field names (`color_primary`, `logo_data_uri`) didn't match
   the real stored keys (`primary_color`, `logo_url`) either.
3. The receipt header read `billing.receipt_branding` as an object where the
   only UI that wrote it sent a boolean.

All three are fixed by consolidating every consumer onto
`internal/platform.LoadIdentity`, the one corrected reader — see contracts
C1/C4/C5 in `00-phase.md`.

**One new bug found and fixed, discovered while implementing this phase's
own contract (not a branding-specific bug — it blocked every multipart
upload endpoint in the product):** `internal/httpapi`'s global `enforceJSON`
middleware rejected **any** `multipart/form-data` request with `415`,
unconditionally, for every mutating route in the API. Found while wiring
`POST /api/v1/settings/branding/logo` (which the phase's own contract C3
requires to be multipart) and getting a `415` through the real router even
though the handler itself was correct. Investigating further: this was not
new to this phase — `internal/billing/ticket_attachments.go`'s payment-proof
upload (v2-2, FR-78.2) and `internal/portalapi`'s `submitTicketHandler` both
accept multipart today, but **every existing test exercising them calls the
Go function directly** (`billing.SubmitTicket`, bypassing HTTP entirely,
per `internal/billing/tickets_gate_test.go`'s own comment) — nothing in the
existing suite had ever sent a real multipart request through the live
`httptest.Server` router before this phase's tests did. Fixed in
`internal/httpapi/router.go`: `enforceJSON` now accepts a `Content-Type`
prefix of `multipart/form-data` alongside `application/json`. Verified
no regression: `TestContentTypeEnforced` (`internal/httpapi`) and the full
`internal/httpapi` suite stay green; `text/plain` and other bodies are still
rejected. Recorded in `docs/ops/known-issues.md`.

**Two already-documented `internal/portalapi` test failures**
(`TestMockGatewayLifecycleReplayAndDisabled`, `TestPortalCardPaymentSubmitAndQueue`,
dated 2026-07-18, retired-route 404s from v2-2) were observed during the full
regression run and are unrelated to this phase — `internal/portalapi`'s only
change this phase is the fixed `GET /api/v1/branding`/new `GET
/api/v1/branding/logo` handlers, neither of which either failing test
exercises.

## Implementation notes

- **Contracts matched the freeze exactly**, including the mid-review
  addition of C10 (fixed attribution) before any code existed for it.
- **Logo storage follows the exact precedent**
  `internal/billing/ticket_attachments.go` already established for
  disk-backed uploads (env-var-with-default directory, magic-byte sniffing,
  atomic write) — a deliberate reuse, not a coincidence, per the phase
  brief's own C2.
- **`PoweredByFooter` is duplicated, not shared**, across `frontend/panel`
  and `frontend/portal` — a deliberate redundancy (C10): a shared component
  would mean a rebrand could be achieved by patching one file instead of
  two, which is a strictly smaller barrier than the fixed-attribution
  guarantee intends.
- **The panel's `BrandedManifestLink.tsx` does its own independent fetch**
  rather than reading the new `branding.tsx` context (unlike the portal's,
  which already read its context) — this is a pre-existing Phase-4
  cross-boundary artifact (see `frontend/panel/src/pwa/README.md`), and the
  phase brief's C7 explicitly scoped this pass to "re-verify, do not
  rebuild." Left as-is; a follow-up could unify it with the context for one
  fewer redundant request per load, but that's out of this phase's stated
  scope.
- **`frontend/panel/src/hooks/useBranding.ts` (the old bare-fetch hook only
  `PrintHeader.tsx` used) was deleted**, replaced by the new
  `frontend/panel/src/branding.tsx` context — per C6/C8, `PrintHeader.tsx`
  needed only its import line changed, no rendering-logic change.
- **`platform.BrandingDir()`'s `sync.Once` resolution is process-global**,
  which every new test that touches `StoreLogo`/`ReadLogoBytes` had to
  account for explicitly (`t.Setenv("HIKRAD_BRANDING_DIR", t.TempDir())`
  before the first call in that test binary, or a package-level `TestMain`
  for `internal/platform`'s own suite) — otherwise a test run would write
  real files under the repo tree's default `data/branding/` path. Documented
  here for whoever next adds a branding-touching test to any of these four
  packages.

## Test-harness notes (for whoever runs this next)

- `internal/billing/db_test.go` gained a blank import of
  `internal/platform/setupapi` (previously absent) so this phase's receipt
  test could write settings through the real HTTP endpoint — the same
  `platform.Settings` instance the billing module itself reads, avoiding a
  same-process stale-cache mismatch a second independent
  `platform.NewSettings(db)` instance would hit (each `Settings` instance
  owns its own read cache; only the server's own instance's writes are
  guaranteed visible to its own subsequent reads — a real characteristic of
  the settings service, not a bug, but worth knowing before reaching for a
  second instance in a test).
- `internal/radius`'s `autoSetupTestModule(t)` helper does not wire
  `m.settings` (its own tests never needed it) — this phase's Hotspot test
  sets it explicitly (`m.settings = platform.NewSettings(m.db)`) after
  calling the helper.
