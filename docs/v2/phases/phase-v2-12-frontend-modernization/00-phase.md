# Phase v2-12 — Frontend Modernization (LAST v2 phase)

Source brief: [docs/v2/12-frontend-modernization.md](../../12-frontend-modernization.md).
Requirements FR-94 (modern control system), FR-95 (responsive/overflow audit),
FR-96 (polish sweep), committed by PRD Decision 44. All three owned entirely
by sub-PRD [07-subscriber-portal-pwa.md](../../../prd/07-subscriber-portal-pwa.md)
— cross-cutting, no domain split, same precedent as FR-54's PWA packaging
(which already spans both apps under one owner). Every other sub-PRD
(02–06, 08) is a **consumer** of this phase's output, never an owner, the
same relationship those modules already have with 07's NFR-6 localization
rules.

**Confirmed before this file was written:** every other v2 phase — v2-1,
v2-3, v2-4, v2-9, v2-2, v2-5, v2-7, v2-6, v2-10, v2-11 — is Complete per
`docs/v2/phases/00-v2-execution-plan.md`'s table. This phase runs **last**
(Decision 32/35) specifically so nothing built afterward can re-introduce a
native control or an overflow bug.

## 1. Problem (restated from the source brief, sharpened by kickoff research)

The component-library decision has been "Tailwind CSS + Radix UI, CSS
logical properties only" since Phase 1 (`frontend/panel/package.json`'s own
comment), but it was never fully executed:

1. **`frontend/panel/src/components/form.tsx`** — the one shared form module
   both apps' screens import from (`'../../components/form'`, ~9 panel
   screens/components today: `SubscriberFormModal`, `NasWizardModal`,
   `NasAutoSetupModal`, `NasScopePicker`, `RoleMatrix`, `UserListPage`,
   `MyPreferencesPage`, and its own test) — is a **single file**, not the
   directory the v2-12 kickoff prompt assumed, and every control it exports
   is a bare native element with only Tailwind utility classes on top:
   `<select>` (`Select`), `<input type="checkbox">` (`Checkbox`), plain
   `<input>`/`<textarea>` (`TextInput`/`Textarea`). None of the two Radix
   packages already installed (`@radix-ui/react-dialog`,
   `@radix-ui/react-dropdown-menu`) are used by it.
2. **`NasScopePicker.tsx`** hand-rolls its own popover-with-checkboxes
   multi-select (a real combobox use case, FR-94's "combobox where lists are
   long" case) entirely from scratch — outside-click/Escape handling,
   `role="listbox"`/`aria-multiselectable` by hand, a bare
   `<input type="checkbox">` per row (line 127) — none of it Radix-backed.
3. **`GlobalSearch.tsx`** similarly hand-rolls a combobox
   (`role="combobox"`/`role="listbox"`/`aria-selected`, arrow-key
   navigation) from a bare `<input type="search">`. This one is **out of
   scope for a rebuild** (see C6) — its keyboard contract (`/` to focus,
   ↑/↓/Enter/Esc) is bespoke and product-critical (NFR-5 keyboard-first
   global search), and FR-96's behavior-preserving constraint means this
   phase restyles what it can prove is safe and does not risk regressing a
   frequently-used, already-correct keyboard flow just to swap its
   internals onto a primitive that offers no behavior it's missing.
4. **Portal has zero Radix dependencies today** (`frontend/portal/package.json`)
   and zero native `<select>`/checkbox/radio usage in its own screens today
   (verified by repo search) — but it will need the same control set the
   moment any future portal screen needs one, and FR-94 explicitly commits
   the shared control system to both apps now, not reactively later.
5. **`docs/ops/known-issues.md`'s 2026-07-17 layout row** is open,
   explicitly assigned to this phase: "Legacy (OS-chrome) scrollbar appears
   when the page can't fit the viewport... No repo-wide responsive audit was
   ever an FR." v1.1's fix (slim/theme-colored scrollbars) was cosmetic only
   — the underlying overflow bugs were never found or fixed.
6. **Panel has no stylelint config**; `frontend/shared/stylelint.config.mjs`'s
   own doc comment says "the panel is invited to adopt it at merge" — an
   invitation never taken up. Portal and shared both enforce the
   logical-properties-only rule (NFR-6.2); the panel does not, and relies on
   convention alone.

This phase's job is therefore not "add a component library" from zero — it
is: **finish** the Radix adoption the project already committed to, replace
every remaining native-chrome control across both apps, close the 2026-07-17
audit debt, and extend the existing RTL lint guarantee to the one app that
doesn't have it yet.

## 2. Scope for this implementation pass

1. **Shared control set** — `frontend/panel/src/components/form.tsx` becomes
   `frontend/panel/src/components/form/` (a directory; C1–C3); portal gets
   its own thin `frontend/portal/src/components/form/` built on the same
   Radix primitives (new dependency for portal); genuinely shared, styling
   -only pieces move to `@hikrad/shared/src/ui/` (C2).
2. **Panel adoption** — every panel screen/component using a bare native
   control (`form.tsx`'s own callers, plus `NasScopePicker.tsx`,
   `SubscriberFormModal.tsx`, `NasAutoSetupModal.tsx`, `NasWizardModal.tsx`,
   `RoleMatrix.tsx`, `UserListPage.tsx`, `MyPreferencesPage.tsx`) switches to
   the new control set. `NasScopePicker` rebuilds its popover on the new
   Combobox/Popover primitive (C6), preserving its exact chip/"any NAS"/
   indented-service behavior byte-for-byte (FR-96.3, behavior-preserving).
3. **Portal adoption** — `ThemeSwitcher`/`LanguageSwitcher` and any future
   portal screen route through the new control set; `SettingsPage.tsx`'s
   existing plain `<input>` fields (text/tel/email/password — never in the
   negative-scope list, see C4) are restyled onto the shared `TextInput` for
   consistency, not because they were ever a CI violation.
4. **Responsive/overflow audit** — every panel + portal screen, 360/768/1280
   (C7); fixes are container-level (`overflow-x-auto` on the wide element,
   form wrapping, modal sizing) — no navigation or information-architecture
   changes.
5. **Polish sweep** — spacing/empty/loading/focus/motion/dark-parity (C8),
   built on the existing `@hikrad/shared` primitives (`states.tsx`, `ui.css`),
   not a second system.
6. **Panel stylelint** — panel adopts `frontend/shared/stylelint.config.mjs`
   (already portal's pattern) and wires it into `npm run lint` (C9).
7. **Gate** — the CI no-native-controls script, a responsive smoke test,
   component tests for the new control set, full existing-suite regression,
   `scripts/gate-v2-phase-12.sh`, `gate-result.md`; closes the
   known-issues 2026-07-17 layout row.

Commit in reviewable chunks, per the kickoff prompt's stated order:
**shared control set (C1–C3) → panel adoption, screen-group by
screen-group (C5–C6) → portal adoption (C6) → responsive/overflow audit
(C7) → polish sweep (C8–C9)**. Each screen-group commit keeps the app
buildable and the existing test suite green, so a regression bisects to one
commit.

## Migration budget

**None.** Frontend-only; no schema, no backend endpoint change (FR-94's
scope note; the source brief's own "Impact map" already states this).

## Frozen contracts

### C1. Module layout — both apps get `src/components/form/`

`frontend/panel/src/components/form.tsx` is replaced by
`frontend/panel/src/components/form/index.ts`, a barrel re-exporting one
file per control (`Select.tsx`, `Combobox.tsx`, `TextInput.tsx`,
`Textarea.tsx`, `Checkbox.tsx`, `Radio.tsx`, `Switch.tsx`, `FileInput.tsx`,
`Field.tsx`) — **the public import path `'../../components/form'` (or
`'./form'`/`'../form'` per caller depth) does not change**, so no consumer's
import line needs editing, only what it imports (if a caller used a name
this phase renames — none are planned; every existing exported name
`Field`/`TextInput`/`Textarea`/`Select`/`Checkbox` is kept and behaves as a
drop-in replacement with the same required/optional props it has today,
plus new optional ones).

`frontend/portal/src/components/form/` is new (portal owns no such module
today), same per-control file layout, same exported names as the panel's for
consistency, but a portal-local package — see C2 for what actually lives in
`@hikrad/shared` versus each app's own tree.

### C2. What lives in `@hikrad/shared` versus each app

`@hikrad/shared/src/ui/` (already home to `StatusBadge`, `QuotaBar`,
`states.tsx`, `ui.css`) gains **styling-only, dependency-free** pieces: the
shared CSS classes (`hk-*`, extending `ui.css`'s existing convention) any
control's visual chrome needs, and pure-presentation subcomponents with no
Radix import of their own (e.g. a `hk-select__indicator` chevron glyph).
**Radix-wrapping components themselves stay app-local** (`frontend/panel/src/components/form/*.tsx`
and `frontend/portal/src/components/form/*.tsx`, each a thin wrapper around
its own app's own `@radix-ui/react-*` dependency) — `@hikrad/shared` is a
source package with `peerDependencies` on `react`/`react-dom` only (see its
`package.json`); adding a hard Radix dependency there would force it onto
every consumer whether or not that consumer needs the control set, which is
a bigger blast radius than this phase's own acceptance sketch calls for.
This mirrors the existing split: `@hikrad/shared` owns tokens/i18n/format
helpers used everywhere, each app owns its own screen composition — the
control set slots into that same boundary, not a new one.

### C3. New dependencies

Panel (`frontend/panel/package.json`) already has
`@radix-ui/react-dialog`/`@radix-ui/react-dropdown-menu`; this phase adds:
`@radix-ui/react-select`, `@radix-ui/react-checkbox`,
`@radix-ui/react-radio-group`, `@radix-ui/react-switch`,
`@radix-ui/react-popover` (backs the new Combobox — Radix Primitives has no
dedicated Combobox component; Popover + a filtered, keyboard-navigable list
is the standard Radix-idiomatic pattern, and it's exactly what
`NasScopePicker`/`GlobalSearch` already hand-build, so this phase is
formalizing an existing pattern onto a primitive, not inventing one).

Portal (`frontend/portal/package.json`) gains the same five packages (its
first Radix dependencies) plus `@radix-ui/react-dialog` if any portal screen
needs a real modal during adoption (none is known to today — added only if
implementation finds a use).

No new dependency outside the `@radix-ui/*` scope (no `react-select`,
`downshift`, `cmdk`, or similar) — Radix primitives plus this phase's own
thin composition covers every control FR-94 lists.

### C4. The no-native-controls CI gate

`scripts/lint-no-native-controls.sh` — a new standalone script, same shape
and calling convention as the existing `scripts/lint-vendor-isolation.sh`
(FR-17's guard): a plain grep, no AST/ESLint-plugin dependency, run directly
by the gate script and wired into each app's own `npm run lint` (panel's
`package.json` `"lint"` script gains `&& sh ../../scripts/lint-no-native-controls.sh panel`,
portal's the same with `portal`).

**Negative scope — exactly these three, matching FR-94.3 and the source
brief's own acceptance sketch, no broader:**
- `<select` (any bare native select)
- `type="checkbox"` / `type='checkbox'`
- `type="radio"` / `type='radio'`

**Not in scope for this gate:** bare `<input type="text"/"tel"/"email"/
"password"/"search"/"number"/"date">` — these already render with only
Tailwind chrome (no OS-native popup/tick the way select/checkbox/radio do),
FR-94.1's TextInput/number/date wrapping is a styling consistency pass on
them (covered by the adoption commits, C5/C6), not a CI-gated negative
invariant; gating them would also false-positive on
`GlobalSearch.tsx`'s intentionally-kept-as-is `type="search"` input (see
problem-statement item 3).

**Exemptions** (grep excludes these paths): each app's own
`src/components/form/**` (the control library's own implementation, which
necessarily contains the real native element Radix renders underneath), and
any `*.test.tsx`/`*.test.ts` file (tests may legitimately assert on
underlying DOM shape, e.g. Radix's hidden `BubbleInput` for native form
submission compatibility — that hidden input lives inside `node_modules`,
never repo source, so this exemption is a safety margin, not a loophole
being relied on).

```sh
# scripts/lint-no-native-controls.sh <panel|portal>  (also runs both if no arg)
# Greps frontend/<app>/src for bare native select/checkbox/radio outside the
# control library's own files and test files. Exit non-zero on any hit.
```

### C5. Panel adoption inventory (behavior-preserving)

Every current consumer of `form.tsx` keeps working via C1's import-path
stability. The following additionally get non-form-module native controls
replaced (found by this phase's own kickoff grep, `<select|type=.checkbox|type=.radio`
under `frontend/panel/src`):
`NasScopePicker.tsx` (line 127, one `<input type="checkbox">` per option
row), `SubscriberFormModal.tsx`, `NasAutoSetupModal.tsx`,
`NasWizardModal.tsx`, `RoleMatrix.tsx`, `UserListPage.tsx`,
`MyPreferencesPage.tsx`. Each conversion is 1:1 — same field, same
validation, same `errors`/`field_errors` wiring (the `Field` wrapper's
`error`/`hint` contract, unchanged) — never a chance to also change what a
screen does, per FR-96.3's behavior-preserving constraint. If kickoff
research at implementation time finds additional native-control usages this
list missed, they are still in scope (the CI gate in C4 is the actual
source of truth for completeness — this inventory is a starting map, not an
exhaustive contract).

### C6. Combobox — the one non-trivial rebuild, and what "behavior-preserving" means for it

`NasScopePicker`'s multi-select popover is rebuilt on `@radix-ui/react-popover`
+ the new `Checkbox` (C3), gaining real focus trapping and Escape handling
from Radix instead of the hand-rolled `mousedown`/`keydown` listeners —
this is allowed to **improve** keyboard/a11y behavior (Radix's baseline is
strictly better than the hand-rolled version), but must preserve every
product-visible behavior byte-for-byte: the chip list, the "any NAS" empty
state, whole-NAS-vs-per-service toggle semantics (`toggleScope`'s existing
narrowing/dropping rules, untouched pure function), and indented service
rows. `toggleScope`/`buildOptions`/`scopeKey`/`labelForScope` (the exported
pure helpers, already covered by `NasScopePicker.test.tsx`) are **not**
rewritten — only the rendering shell around them changes.

`GlobalSearch.tsx` is explicitly **out of scope for a primitive-based
rebuild** (see problem statement item 3) — it keeps its bespoke
implementation as-is, restyled only if the responsive/polish passes (C7/C8)
find a real issue in it, never structurally rebuilt in this phase. This is
a deliberate, narrow exception to FR-94.1's "used everywhere": the source
brief's own acceptance sketch requires "no native chrome," and
`GlobalSearch.tsx` already has none (it's a styled `<input type="search">`,
not a `<select>`/checkbox/radio) — the CI gate (C4) does not fire on it, so
leaving it alone is compliant, not a gap.

### C7. Responsive/overflow audit — method and breakpoints

**Breakpoints:** 360px (phone floor, Hassan's persona), 768px (tablet),
1280px (desktop) — matching the source brief's acceptance sketch exactly.

**Rule:** `document.body`'s `scrollWidth` must never exceed its
`clientWidth` at any of the three widths, on any screen, in either app. A
screen whose content is legitimately wider than the viewport (a data table,
a usage chart, a config/snippet block, a JSON debug view) gets that specific
element wrapped in its own `overflow-x-auto` container — the element
scrolls, the page does not.

**Verification, two tiers:**
- **Automated (cheap, jsdom):** a new `layoutSmoke.test.tsx` per app,
  rendering a representative sample of routed screens (every top-level
  route at minimum) at each of the three widths via a `matchMedia`/
  `ResizeObserver` stub already common in this codebase's test setup, and
  asserting no element outside an explicitly `overflow-x-auto`-classed
  container reports a wider `scrollWidth` than its parent. This is a smoke
  test, not a pixel-perfect layout assertion — jsdom does not do real
  layout, so the assertion is structural (an `overflow-x-auto` class is
  present on the known-wide containers: tables, `UsageChart`, `Sparkline`,
  report print views, NAS config/snippet blocks) rather than a computed
  measurement.
- **Manual matrix (documented in the gate result, not scripted):** the full
  360/768/1280 × light/dark × LTR/RTL spot-check across every screen group,
  recorded as a table in `gate-result.md` (screen → breakpoint → pass/fail
  → note), same evidentiary posture as prior phases' human/hardware legs.

### C8. Polish sweep scope

Builds on existing primitives, does not replace them:
- **Spacing:** Tailwind's default scale, applied consistently — no new
  custom spacing tokens.
- **Empty/loading states:** every list/table screen that doesn't already use
  `@hikrad/shared`'s `EmptyState`/`LoadingState` (`states.tsx`) adopts them;
  screens that already do are left alone.
- **Focus rings:** every interactive element (including every new control
  from C1–C6) has a visible focus ring meeting WCAG 2.2 non-text-contrast
  (Radix's default focus-visible behavior, not suppressed by an
  `outline-none` without a replacement — `form.tsx`'s current
  `focus:outline-none` on `CONTROL` without a `focus:ring-*` replacement is
  a real pre-existing gap this phase closes).
- **Motion:** any new transition this phase adds (control open/close,
  popover enter/exit) wraps in a `prefers-reduced-motion` media query or
  Tailwind's `motion-reduce:` variant; existing CSS-only animations
  (`.hk-quota__fill`, `.hk-spinner`) are left as-is unless they're found to
  violate this during the audit (spinners are conventionally exempt from
  reduced-motion since they signal indeterminate wait state, not decorative
  motion — no change planned).
- **Dark/light parity:** spot-checked per screen group as part of C7's
  manual matrix, not a separate pass.

No new user-visible copy is required by this contract; if implementation
needs one (e.g., a combobox "no results" string), it ships trilingual
(en/ar/ku) in the same commit per NFR-6.1, and `npm run i18n:check` stays
green throughout — not just at the end.

### C9. Panel stylelint adoption

`frontend/panel/stylelint.config.mjs` is added, re-exporting
`@hikrad/shared/stylelint.config.mjs` verbatim (identical to
`frontend/portal/stylelint.config.mjs`'s existing one-line re-export).
`frontend/panel/package.json`'s `"lint"` script gains
`&& stylelint "src/**/*.css"` (matching portal's existing `"lint"` script
shape exactly). This closes `stylelint.config.mjs`'s own long-standing
"the panel is invited to adopt it at merge" comment note, which is updated
to state the panel now enforces it too.

## Integration gate

Green when all pass (scriptable legs in `scripts/gate-v2-phase-12.sh`; no
DB/Redis dependency anywhere in this phase — frontend-only):

1. **No schema change** — no new migration file above the repo's current
   maximum; `go build`/`go vet` unaffected (backend untouched).
2. **No-native-controls grep gate green (FR-94.3, C4)** —
   `scripts/lint-no-native-controls.sh` exits 0 for both apps.
3. **New control set exists and is tested** — a component test per control
   (`Select`, `Combobox`, `TextInput`, `Textarea`, `Checkbox`, `Radio`,
   `Switch`, `FileInput`) in `frontend/panel/src/components/form/*.test.tsx`,
   asserting it renders via its Radix primitive (not a bare native element,
   `container.querySelector('select')` etc. returns null where applicable)
   and forwards its documented props (`error`, `disabled`, `value`/
   `onChange`).
4. **Panel adoption complete** — every file listed in C5, plus any the
   grep gate (item 2) still catches, uses the new control set; no
   regression in each screen's own existing test suite.
5. **`NasScopePicker` behavior-preserving (C6)** — `NasScopePicker.test.tsx`
   (existing) stays green unmodified in its assertions (only its rendering
   queries may need updating for the new DOM shape, never its expected
   outcomes) — chip add/remove, "any NAS" default, whole-NAS-vs-service
   narrowing all still pass.
6. **Portal adoption** — portal's `package.json` carries the new Radix
   dependencies; `frontend/portal/src/components/form/` exists with the
   same control set; a portal component test exercises at least one control
   end-to-end (e.g. `ThemeSwitcher` or a new portal screen using `Select`).
7. **Responsive smoke test green (FR-95, C7)** — `layoutSmoke.test.tsx` in
   both apps passes at 360/768/1280.
8. **Known-issues row closed (FR-95.1)** — the 2026-07-17 "Panel+portal /
   layout" row in `docs/ops/known-issues.md` has its Status updated to
   reflect the fix (append-only discipline: updated, not deleted), citing
   this phase's gate result.
9. **Panel stylelint adoption (C9)** — `frontend/panel/stylelint.config.mjs`
   exists and re-exports the shared config; `npm run lint` (panel) runs
   stylelint and passes; `stylelint.config.mjs`'s own comment is updated.
10. **Focus rings present (C8)** — a component test asserts a visible
    `:focus-visible` style (a Tailwind ring class or equivalent) on at least
    one instance of each new control.
11. **Reduced motion respected (C8)** — any new transition CSS this phase
    adds is guarded by `prefers-reduced-motion`/`motion-reduce:` (grep or
    component-test check, implementer's choice, documented in the gate
    result).
12. **Panel/portal build/lint/i18n** — `npm run build`/`npm run lint`
    (which now includes item 2's grep gate and item 9's stylelint) green for
    both apps; `npm run i18n:check` green (0 hardcoded, 0 missing keys
    across en/ar/ku) — including any new strings this phase introduces.
13. **Full regression** — every existing panel + portal + shared vitest
    suite stays green; no test deleted to make this phase pass (a test
    whose DOM-query assumptions changed due to the new control set may be
    updated, per item 5's "queries may change, outcomes may not" rule).
14. **Docs accuracy** — PRD carries FR-94/95/96; sub-PRD 07 carries all
    three; `docs/prd/00-index.md` shows 96/96 FRs owned (already committed
    in this phase's Step-1 docs commit, verified again here); `CLAUDE.md`
    updated to record v2-12 complete once the gate passes (not part of the
    Step-1 commit — CLAUDE.md is updated at ship time, same convention
    every prior phase followed); any further bug found while building is
    recorded in `docs/ops/known-issues.md`.

**Manual matrix (C7's second verification tier — recorded in
`gate-result.md`, not scripted):** 360/768/1280 × light/dark × LTR/RTL
spot-check across every screen group in both apps.

Human/hardware legs: **none** — no router/device/real-browser dependency
beyond the manual matrix above, which needs only a browser resize (or
devtools device emulation), no physical hardware. Same posture as
v2-4/v2-6/v2-9/v2-10/v2-11.

## Open implementation questions for whoever builds this (not blocking)

- **Combobox for other long-list pickers:** the source brief mentions
  "combobox where lists are long (NAS/profile pickers)" beyond
  `NasScopePicker`. Kickoff research found `NasScopePicker` as the one
  existing hand-rolled instance; if implementation finds another
  screen with a long native `<select>` of NAS/profiles (as opposed to a
  short, fixed enum — those stay a plain `Select`), converting it to
  Combobox is in scope for FR-94.1's "used everywhere" even though it isn't
  individually named in C5's starting inventory.
- **Portal's first Radix `Dialog`:** C3 notes this is added only if
  adoption finds a real use. If it turns out unnecessary, don't add the
  dependency for its own sake — note the decision in the gate result.
- **`FileInput`'s relationship to the existing payment-ticket attachment
  upload** (`internal/billing/ticket_attachments.go`'s multipart consumer,
  v2-2 FR-78.2, and the branding logo upload, v2-11 FR-91) — this phase's
  `FileInput` is the front-end control (styling/drag-drop affordance/file
  list), not a new upload endpoint; it should compose with whichever
  existing upload call site it's dropped into (portal payment-ticket
  attachments, panel branding logo) without changing those endpoints'
  contracts. Confirm at implementation time whether either call site
  benefits from adopting it, or whether it's added but not yet wired
  anywhere pre-existing (acceptable — FR-94 requires the control to exist
  and be used "everywhere" a file input already renders; if today's file
  inputs there are already reasonably styled, restyling them is still in
  scope, just lower-risk than the select/checkbox/radio conversions).
