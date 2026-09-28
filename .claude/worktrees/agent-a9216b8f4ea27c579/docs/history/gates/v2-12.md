# Phase v2-12 — Frontend Modernization — Integration Gate Result

Run date: 2026-07-18. Executed **solo + sequential** per the v2 execution
plan, in the committed order: shared control set (C1–C3) → panel adoption
(C4–C6, C9) → portal adoption (C1–C3, C6) → responsive/overflow audit
(C7) → polish sweep (C8), five reviewable commits. Every other v2 phase
(1, 3, 4, 9, 2, 5, 7, 6, 10, 11) was confirmed Complete before kickoff, per
the execution plan's "v2-12 must run last" rule — this is the final v2
phase.

Verification environment: frontend-only phase, no DB/Redis dependency.
`go build`/`go vet` confirm the backend is untouched.

`scripts/gate-v2-phase-12.sh`: **38/38 scripted legs PASS.**

## Gate items

| # | Item | Result | Evidence |
|---|---|---|---|
| 1 | **No schema change** | **PASS** | Backend untouched by this phase; `go build`/`go vet ./...` clean. |
| 2 | **No-native-controls grep gate green (FR-94.3, C4)** | **PASS** | `scripts/lint-no-native-controls.sh` (new, same shape as `scripts/lint-vendor-isolation.sh`) exits 0 for both apps; wired into each app's own `npm run lint`. |
| 3 | **New control set exists and is tested** | **PASS** | `frontend/panel/src/components/form/{Select,Combobox,TextInput,Textarea,Checkbox,Radio,Switch,FileInput}.test.tsx` — 31 tests, all green; each control renders via its Radix primitive (no bare native element), forwards `error`/`disabled`/`value`/`onChange`. |
| 4 | **Panel adoption complete** | **PASS** | Every file in the phase brief's C5 inventory, plus one real gap the grep gate itself found (`NasAutoSetupModal.tsx`'s `ResolutionChoice`, not anticipated at kickoff), uses the new control set; `NasScopePicker.test.tsx` (8 tests) and `RoleMatrix.test.tsx` (3 tests) both pass unmodified. |
| 5 | **`NasScopePicker` behavior-preserving (C6)** | **PASS** | All 8 existing tests pass with **zero assertion changes** — chip add/remove, "any NAS" default, whole-NAS-vs-service narrowing (`toggleScope`) all still hold against the Combobox rebuild. |
| 6 | **Portal adoption** | **PASS** | `frontend/portal/package.json` carries the five new `@radix-ui/*` dependencies (portal's first); `frontend/portal/src/components/form/` exists with the identical control set (same design tokens make the panel's component code work verbatim); its own 31-test suite is green. |
| 7 | **Responsive smoke test green (FR-95, C7)** | **PASS** | `layoutSmoke.test.tsx` in both apps — scans source for `<table>`/`<pre>` and asserts each has an `overflow-x-auto`/`overflow-auto` ancestor nearby; 0 violations found (every existing wide-content container, 17 files, was already correctly wrapped). |
| 8 | **Known-issues row closed (FR-95.1)** | **PASS** | The 2026-07-17 "Panel+portal / layout" row's Status no longer starts with "Open —"; updated in place (append-only discipline) to record what was actually verified vs. left documented-pending (see below). |
| 9 | **Panel stylelint adoption (C9)** | **PASS** | `frontend/panel/stylelint.config.mjs` re-exports `@hikrad/shared`'s logical-properties config (identical to portal's existing one-line re-export); `npm run lint` runs it; passes clean. |
| 10 | **Focus rings present (C8)** | **PASS** | `focusRing.test.tsx` (both apps) — every new control (`TextInput`, `Select`, `Checkbox`, `RadioOption`, `Switch`, `Combobox` trigger) carries a `focus-visible:ring-2` class; `form.tsx`'s pre-modernization `CONTROL` constant had `focus:outline-none` with **no** replacement ring — a real, closed a11y gap. |
| 11 | **Reduced motion respected (C8)** | **PASS** | `shared.ts`'s `POPOVER_CONTENT` and `Switch.tsx`'s thumb transition both carry `motion-reduce:transition-none`; grep confirms at least one file in each app's control-set directory contains the guard. |
| 12 | **Panel/portal build/lint/i18n** | **PASS** | `frontend/shared`/`panel`/`portal` build clean; both apps' `npm run lint` (ESLint + Prettier + stylelint + the new no-native-controls gate) exit 0 — pre-existing `react-refresh/only-export-components` warnings only, 0 errors; `i18n:check` green across en/ar/ku (0 hardcoded strings, 0 missing keys; the one new key, `nasScope.search`, is translated in all three) — the two pre-existing "identical to en" warnings (`common.productName`, `preferences.landingPagePlaceholder`) predate this phase. |
| 13 | **Full regression** | **PASS** | Panel: **113** vitest tests green (was 76 pre-phase — +31 new control-set tests, +4 `SubscriberFormModal.test.tsx`, +2 `layoutSmoke.test.tsx`; one pre-existing test, `BulkBar.test.tsx`, had its Select interaction updated from `fireEvent.change` on a native select to open+click, same assertions/outcome). Portal: **50** vitest tests green (was 17 pre-phase — +31 new control-set tests, +2 `layoutSmoke.test.tsx`). `@hikrad/shared`'s own suite unaffected, green. |
| 14 | **Docs accuracy** | **PASS** | `docs/PRD.md` carries FR-94/95/96 and Decision 44 (Step-1 commit); sub-PRD 07 carries all three with elaboration + acceptance criteria; `docs/prd/00-index.md` shows **96/96** FRs owned; `docs/ops/known-issues.md` gained one new row (the pre-existing `Field`/`htmlFor` label-association gap found while writing this phase's own tests) and closed the layout row; `CLAUDE.md` updated in this same pass to record v2-12 complete (ship-time convention, not part of the Step-1 docs commit). |

## GREEN / RED verdict

**GREEN — 38/38 scripted legs pass** (`scripts/gate-v2-phase-12.sh`).

Human/hardware legs: **one, documented-pending.** The phase brief's C7
committed to two verification tiers — an automated structural smoke test
(delivered, item 7) and a manual **360px/768px/1280px × light/dark ×
LTR/RTL visual matrix** across every screen in both apps. This session has
no browser to drive that matrix in — the "manual" tier here means static
code review (every Tailwind responsive/logical-property class was read,
not rendered), not an actual resized viewport. This is the same posture
prior phases used for a leg needing real hardware/a real browser they
didn't have in-session (e.g. v2-5's clean-VM install, v2-7's button-triggered
update) — recorded as open, not silently assumed passing. Whoever next has
a real browser available should run the matrix; `docs/ops/known-issues.md`'s
layout row and this file both flag it explicitly so it isn't lost.

## Bugs found and fixed

**One real gap in the phase brief's own C5 inventory, found by the grep
gate itself, not anticipated at kickoff:** `NasAutoSetupModal.tsx`'s
`ResolutionChoice` component rendered a bare, **unnamed** `<input
type="radio">` per conflict-resolution choice (abort/keep/update) — three
independent inputs with no `name` grouping them, relying entirely on app
state (not native radio semantics) for mutual exclusivity. Converted to a
real `RadioGroup`/`RadioOption` per conflict item; this also gains real
grouped keyboard navigation the ungrouped version never had. No existing
test covered this file (verified: no test file exists under
`src/pages/nas/`), so this was a silent gap until the grep gate — written
before any implementation, per the standing kickoff protocol — caught it.

**One real, pre-existing, repo-wide accessibility gap, found while writing
this phase's own new test coverage (not previously known):**
`frontend/panel/src/components/form/Field.tsx` renders its `<label
htmlFor={htmlFor}>` as a **sibling** of `children`, not a wrapper — the
association depends entirely on the caller passing a matching `htmlFor`/
`id` pair, which the overwhelming majority of the panel's ~40 `Field` call
sites never did (a sighted developer can't see the missing link; the label
sits visually right above the field either way). Found writing
`SubscriberFormModal.test.tsx`: `screen.getByLabelText(en.subscriber.password)`
threw "no form control was found associated to that label." Confirmed
pre-existing since Phase 1 — `Field`'s `htmlFor` prop has always been
optional and this call site never passed it, unrelated to this phase's
Radix rebuild. **Fixed in the two files this phase was already touching
deeply** (`SubscriberFormModal.tsx`, 14 pairs; portal's `SettingsPage.tsx`,
5 pairs, which additionally lost its *correct* native `<label>`-wrapping
association when its inputs moved onto `Field` — caught and fixed in the
same commit). **Not fixed repo-wide** (~35 other call sites) — recorded in
`docs/ops/known-issues.md` as a mechanical, low-risk, out-of-scope-for-this-phase
follow-up (add `id`/`htmlFor` pairs; no visual change).

## Implementation notes

- **Behavior-preserving by construction, verified, not just asserted**: every
  existing test that exercised a converted control (`NasScopePicker`,
  `RoleMatrix`, `BulkBar`) either passed with zero assertion changes or (for
  `BulkBar`) had only its *interaction mechanics* updated (open+click instead
  of `fireEvent.change`, since Radix Select renders a button, not a
  `<select>`) — never its expected outcome. No feature behavior changed.
- **Select's empty-string `<option value="">` pattern** (~15 call sites
  across the panel: "no profile," "any NAS," etc.) is preserved via an
  internal sentinel string, since Radix Select forbids an `Item value=""`
  directly — transparent to every caller, none of whom needed to change.
- **`Combobox` gained a search box** `NasScopePicker`'s original hand-rolled
  popover never had — a deliberate, additive enhancement within C6's
  "allowed to improve keyboard/a11y behavior" allowance, not a strict
  byte-for-byte parity claim for this one piece (every *other* behavior —
  chips, narrowing, indentation, empty state — is unchanged).
- **`@hikrad/shared` gained no new hard dependency.** Per contract C2, the
  Radix-wrapping components stay app-local (each app's own `@radix-ui/*`
  dependency); only styling-only, dependency-free pieces would go in
  `@hikrad/shared/src/ui/` — none were needed this phase, since both apps'
  identical Tailwind token names let the same component *source* be copied
  verbatim instead.
- **`PoweredByFooter`-style deliberate duplication is not this pattern**:
  unlike v2-11's footer, the control set genuinely is copied because Radix
  is a real per-app dependency, not a design choice to raise a barrier.
- **jsdom Radix polyfills** (`ResizeObserver`, `hasPointerCapture`/
  `setPointerCapture`/`releasePointerCapture`, `scrollIntoView`) already
  existed in the panel's `src/test/setup.ts` from earlier Dialog/
  DropdownMenu usage; portal's test setup needed the same block added
  (portal had zero Radix dependencies before this phase).
- **`layoutSmoke.test.tsx` was verified to actually catch a regression**,
  not just written and trusted: a wrapper's `overflow-auto` class was
  temporarily removed, the test was confirmed to fail, and the file was
  restored — the same discipline `docs/ops/known-issues.md`'s "how bugs are
  found" convention expects of any new regression-locking test.
