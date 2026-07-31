# Phase v3-3 — Licensing Hardening + Keygen CLI — Gate Result

**Verdict: GREEN.** 9/9 scripted gate items PASS; 1 leg (live-browser visual confirmation of the banner/labels across light/dark × en/ar/ku) is documented-pending — no browser available in-session, same posture as v2-12's visual matrix.

Date: 2026-07-22 · FR-103–105 (PRD Decisions 48–49) · owned by sub-PRD 01 · **no migration** (repo max stays 0592).

## What shipped

| Contract | Summary | Key files |
|---|---|---|
| C1 | Interactive keygen `issue` mode (guided prompts + tier menu + custom option); non-interactive flag mode retained for CI; byte-identical signed blob via a single `signAndWrite` path | `scripts/license-tool/main.go`, `scripts/license-tool/main_test.go` |
| C2 | Tier catalog `starter`=200 / `standard`=500 / `pro`=1000 / `unlimited`=0 (owner-confirmed) | `scripts/license-tool/main.go` (`tierCatalog`) |
| C3 | Subscriber-cap enforcement: hard wall + `max(10, ⌈5%⌉)` buffer, at create **and** CSV import (dry-run warns, execute refuses overflow rows via inherited 403), all non-deleted subscribers counted | `internal/platform/license_store.go` (`EvalSubscriberCap`, cached cap), `internal/subscribers/api.go`, `internal/importer/validate.go` |
| C4 | Current fingerprint + per-component drift diagnostic on License page and setup wizard; cap status surfaced for a persistent panel prompt | `internal/platform/license/license.go` (`CompareComponents`), `internal/platform/setupapi/license_api.go`, `frontend/panel/src/pages/license/LicensePage.tsx`, `frontend/panel/src/api/setup.ts` |
| C5 | `warn` color token (light+dark) fixes the invisible grace banner; `license.state.*` i18n restructured (all 3 locales) fixes the raw-key labels | `frontend/panel/tailwind.config.ts`, `frontend/panel/src/theme/tokens.css`, `frontend/shared/locales/{en,ar,ku}/panel.json` |

## Gate items

1. **C1 round-trip & byte-identical — PASS.** `scripts/license-tool` tests green (`go test ./...` → `ok hikrad-license-tool`): `TestSignAndWrite_VerifiesAndDecodes` (blob verifies under the vendor key + decodes), `TestSignAndWrite_ByteIdenticalAcrossPaths` (flag-path and interactive-path `issueInput` → identical bytes), `TestSignAndWrite_UnlimitedTier`. Flag mode unchanged (any flag present → the exact prior path).
2. **C2 tier catalog — PASS.** `TestTierCatalog` asserts the four owner-confirmed tiers/values; the menu pre-fills `max-subscribers` from the tier, `unlimited`→0, plus a custom option.
3. **C3 enforcement — PASS.** `internal/platform` `TestEvalSubscriberCap` covers under-cap / warn-band / blocked and the buffer floor at 10 for small caps; create refuses with `license_subscriber_cap` (403) past cap+buffer and sets `X-HikRAD-Warning: license_over_cap` in the band; unlimited/absent license skips the COUNT entirely. Edits/renewals never gated (enforcement is only on the create path).
4. **C3 import — PASS.** Import self-dispatches creates through the real subscribers router, so the 403 is inherited: dry-run appends a per-row "exceeds licensed cap" warning mirroring execute's real behavior; execute marks overflow rows `failed` with the cap message (no silent truncation).
5. **C4 fingerprint — PASS.** `GET /api/v1/license` + `/setup/license` now include `fingerprint_match` (machine-id/MAC/within-tolerance); `license.CompareComponents` unit-tested (`TestCompareComponents`) for machine-id-only, MAC-only, both-changed, and malformed inputs; License page renders the copyable current fingerprint + drift diagnostic; wizard already displayed the current fingerprint (verified).
6. **C5 banner — PASS.** `--hik-warn` token added (light `180 83 9` / dark `245 176 80`, contrast-checked under `text-ink-inverse`) + `warn` Tailwind color; the grace banner now renders a real background. `tsc --noEmit` clean; panel `layoutSmoke.test.tsx` green.
7. **C5 labels — PASS.** `license.state` restructured to an object in en/ar/ku; License page uses `license.state.label` for the row label and `license.state.${state}` for the value. `frontend/shared/src/i18n/i18n.test.tsx` locks that every `license.state.*` key resolves (not to the raw key) in all three locales. `npm run i18n:check` → **OK** (my new keys fully translated; the 5 residual identical-string warnings are pre-existing proper-noun keys, unrelated).
8. **v2-12 CI floors — PASS.** `no-native-controls (panel): clean`; prettier clean; stylelint logical-properties passes; `i18n:check` OK; both apps' layout smoke green. No native control added.
9. **No regressions — PASS.** Backend `go build ./...` + `go vet ./...` clean; all changed-package tests green (`platform`, `platform/license`, `platform/setupapi`, `subscribers`, `importer`). The grace state machine, `WithinTolerance`, `Evaluate`, and FR-50.3 semantics are untouched (display/diagnostics + an additive create-only gate only) — RADIUS/accounting unaffected.

## Documented-pending / notes

- **Live-browser visual matrix** (grace banner + state labels at light/dark × en/ar/ku) — no browser in-session; carried as the human-review half (same posture as v2-12).
- **Pre-existing, unrelated failing tests** surfaced by the full backend run and **confirmed pre-existing by stashing all v3-3 changes**: `cmd/hikrad-updaterd`'s `TestRollbackSurvivesDaemonDeath` and `TestRollbackStatusPersistsAcrossRestart` (process-spawning/timing integration tests; `cmd/hikrad-updaterd` has zero dependency on anything v3-3 touched). Logged in `docs/ops/known-issues.md` (2026-07-22 row). Not a v3-3 regression.
- **Open sub-decisions:** none — all five owner questions answered 2026-07-22 and frozen in `00-phase.md`.
- **DB-gated integration tests** for the create-path 403 and import overflow were not added as new DB tests this pass (the pure `EvalSubscriberCap` + inherited-403 path are unit-covered); a DB-backed create-cap test is a cheap follow-up when next in `internal/subscribers`' DB suite.
