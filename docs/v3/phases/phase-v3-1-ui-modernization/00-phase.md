# Phase v3-1 — Full UI/UX Modernization Program

> Kicked off 2026-07-23 (PRD Decision 53). SOLO, sequential. Owned by **sub-PRD 07** (cross-cutting panel + portal, the FR-94–96 precedent). FR-97/98/99. **No migration.** In-place progressive redesign — no parallel frontend, no framework change, behavior-preserving, shippable after every sub-phase. Brief source: [docs/v3/01-ui-ux-modernization.md](../../01-ui-ux-modernization.md).

## Open questions — resolved at kickoff (recommended defaults adopted)

1. **Sub-phase count = 5** (foundation → subscribers+billing → NAS+monitoring → security/settings+account → portal). Smaller shippable slices.
2. **Dashboard reorder = pointer-events hand-roll with the existing arrow buttons as the keyboard-accessible fallback** — no new drag-and-drop dependency unless the hand-roll proves inadequate at review (decided in FR-99's sub-phase, not before).
3. **Saved filter *views* (named filter sets) = deferred to backlog** — the FilterBar ships with per-manager-persisted *active* filters (the existing `usePersistentState` mechanism), not named saved views.

## Scope

FR-97 (foundation) + FR-98 (screen overhaul) + FR-99 (account area + dashboard UX). No backend feature work, no framework/library migration, no rebranding (FR-91–93 stand, the fixed "Powered by HikRAD" mark stays). No migration.

## Sub-phases (each ends with the full frontend suite green + the app shippable)

- **SP1 — Foundation (FR-97).** Token layer + pattern library + `design-language.md` + `/design` gallery. **Additive** — touches no existing screen, so the existing suite stays green by construction. This is the substrate every later sub-phase adopts.
- **SP2 — Subscribers + billing (FR-98.1/98.2).** Subscribers list FilterBar; subscriber + profile + billing forms re-ordered; billing/payments/vouchers/reports screens adopt the patterns.
- **SP3 — NAS + monitoring.** NAS list/detail/wizard, pools, live sessions, devices/health/alerts adopt the patterns.
- **SP4 — Security/settings + account area (FR-99.1).** Managers/roles/audit/settings adopt the patterns; the "My account" area lands with redirects from the old routes.
- **SP5 — Portal + dashboard UX (FR-99.2).** Portal screens adopt the patterns; dashboard-customization direct-manipulation flow.

## Frozen contracts

### C1. Token layer (FR-97.1) — `theme/tokens.css` + `tailwind.config.ts`, additive
- New CSS custom properties on `:root` (and mirrored in `:root[data-theme='dark']` where they differ): a **spacing** scale is Tailwind's default (kept), a **radius** scale (`--hik-radius-sm/md/lg/xl`), an **elevation** scale (`--hik-shadow-sm/md/lg`, tuned per theme), a **type** scale surfaced as Tailwind `fontSize` tokens. Colors already exist (`--hik-brand/surface/ink/danger/warn/ok`) and are unchanged.
- Tailwind `theme.extend` gains `borderRadius` + `boxShadow` reading the CSS vars, so utilities like `rounded-md`/`shadow-md` resolve to tokens. **No existing token value changes** — this is additive, so no current screen shifts.

### C2. Shared pattern primitives (FR-97.2) — `@hikrad/shared/ui`, pure/presentational
- `Card` (surface-raised container, padded, tokenized radius/elevation) + `StatCard` (label + value + optional delta/hint) — dashboard tiles + detail summaries.
- `DataTable` — a thin composable table: columns spec, sticky header, row density, built-in `LoadingState`/`EmptyState`/`ErrorState` (reuses the existing `states.tsx`), horizontal scroll inside its own `overflow-x-auto` container (AC-95a). No form/control dependency, so it stays in shared.
- `SectionCard` — a titled content section (header + body) for grouping.
- All render token-only classes/`.hk-*` CSS; RTL-correct (logical properties); a11y roles preserved.

### C3. Panel composed patterns (FR-97.2) — `frontend/panel/src/components/patterns/`
- `FilterBar` — a search input + an "add filter" menu (panel `Select`/`Combobox`) producing removable typed **chips**; controlled (`filters` value + `onChange`), field definitions passed in; renders the active-chip row + clear-all. This is the FR-98.1 subscriber-filter target; built here (depends on panel form controls), adopted on the subscribers list in SP2.
- `FormSection` — a titled, optionally-collapsible form group (identity/service/billing/**Advanced** collapsed-by-default) for the FR-98.2 ordering pass; uses the existing `Field`.
- `DetailHeader` — a detail-page header (title + status + meta + actions), used by subscriber/NAS/device detail pages.
- **Label association**: the FR-97 form primitives associate a visible label with its control by construction (generated id + `htmlFor`, or wrapping) — the sweep to the ~35 bare `Field` call sites happens per-screen in SP2–SP5, closing the known-issues 2026-07-18 gap.

### C4. Design-language doc + gallery (FR-97.3)
- `docs/v3/phases/phase-v3-1-ui-modernization/design-language.md` — voice, density, motion (reduced-motion), when-to-use-which-pattern, the "not AI-built" review checklist.
- A dev-only `/design` panel route rendering every pattern; behind the existing dev-route pattern (like `/dev/rtl-smoke`), not in production nav.

### C5. FR-98/99 screen contracts (frozen per-batch in this file as each sub-phase begins)
- **Behavior-preserving invariant**: no endpoint, permission string, or i18n-key *semantics* changes without an explicit amendment to this file. i18n keys may be **added** (trilingual, same commit) but an existing key's meaning is never repurposed.
- **Per-form field-order specs** (FR-98.2) are appended to this file before each form is touched.
- **Account-area routes/redirects** (FR-99.1) and the **dashboard interaction spec** (FR-99.2) are appended before SP4/SP5 code.

## Integration gate (per sub-phase + a phase-final)

1. **Foundation (SP1)** — `/design` renders every pattern in light+dark; a shared `ui` test asserts the new primitives render + a form primitive is label-associated; token additions don't shift any existing screen (existing suite green unmodified).
2. **Per-screen (SP2–SP5)** — the touched screens' existing tests pass unmodified or with **reviewed** updates (a diff explaining any changed assertion); FilterBar persistence + bulk-parity test (AC-98a); each form's field-order matches its frozen spec (AC-98b); account area ≤2 clicks + old-route redirects (AC-99a); dashboard add/reorder/resize keyboard-operable + layout round-trip unchanged (AC-99b).
3. **CI floors green throughout** — `lint-no-native-controls.sh`, both apps' `layoutSmoke.test.tsx`, stylelint logical-properties, `npm run i18n:check`, `tsc`, backend untouched-green.
4. **Phase-final** — full frontend suite green; the `Field`-association known-issues row updated; every new string trilingual; a written per-sub-phase gate note; the **live-browser visual matrix (360/768/1280 × light/dark × LTR/RTL)** documented-pending (no browser in-session, same leg v2-12 left).

## Constraints (may not be relaxed)
In-place progressive redesign; behavior-preserving; trilingual + true RTL on every touched screen (logical properties only, stylelint gate); the v2-12 CI floors are hard floors; SOLO sequential; no migration.

## Progress log

- **2026-07-23 — SP1 (FR-97 foundation) COMPLETE, GREEN.** Token layer extended (radius = Tailwind defaults so zero shift; elevation refined + dark-mode shadow fixed) in both apps' `tokens.css` + `tailwind.config.ts`. Shared primitives `Card`/`StatCard`/`SectionCard`/`DataTable` (`@hikrad/shared/ui`, `.hk-*` CSS, 5 tests). Panel patterns `FilterBar`/`FormSection`+`FormField`(label-associated by construction)/`DetailHeader` (`components/patterns/`, 4 FilterBar tests). `design-language.md` + the dev-only `/dev/design` gallery route (both themes × en/ar/ku). Gate: panel `tsc` clean, **panel 36 files / 121 tests + shared 8 files / 60 tests green**, `i18n:check` OK (1618 keys × 3 locales), panel+portal lint pass (0 errors, `no-native-controls` clean).
- **2026-07-23 — SP2 START — subscribers FilterBar adoption, GREEN.** `UserListPage` row-of-selects + hand-rolled chips replaced by the FR-97 `FilterBar` (search + add-a-filter → removable chips); the new `q` search is wired to both the client filter and the server `bulkFilter` (bulk parity preserved), per-manager `usePersistentState` persistence unchanged, one native `<select>` removed. Subscribers suite (14 tests) + full panel suite green; lint + i18n green.
- **2026-07-24 — SP2 — profile form-ordering pass (FR-98.2), GREEN.** `ProfileFormModal` regrouped into `FormSection`s — Identity & billing → Speed & limits → Advanced (hotspot rates, NAS scopes, address-list), the Advanced section collapsed by default but auto-expanding when editing a plan that already has advanced values. Every field preserved; panel suite (36 files / 121 tests) + tsc + lint + i18n green.
- **2026-07-24 — SP3 start — card standardization, GREEN.** The NAS list and Pools cards (hand-rolled `rounded-lg border … bg-surface-raised p-4` divs) now use the shared `Card` primitive, so they pick up the design-system tokenized elevation (`shadow-sm`) consistently. Behavior-identical; build + 121 tests + lint + i18n green. Also fixed a real dev-server break (`DesignGalleryPage` imported a non-existent `QuickToggles`) + 3 latent test-fixture type errors that only `npm run build`/`tsc -b` catches (see known-issues 2026-07-24) — the frontend gate now runs `npm run build`. Added a **dev-only public `/design` route** (outside the auth guard, `import.meta.env.DEV`) so the pattern gallery is reviewable without a running backend.
- **2026-07-24 — SP4 — "My account" area (FR-99.1), GREEN.** New `/me` landing (`MyAccountPage`) gathering My security (`/account`), My preferences (`/preferences`), My payment methods (`/my-payment-methods`) + admin quick-links (Settings, Managers & roles, permission-gated) as `Card` links; a "My account" entry added to the `UserMenu`. Existing routes unchanged and still work (the landing links to them, ≤2 clicks). Trilingual `account.*`; build + 121 tests + lint + i18n green.
- **2026-07-24 — SP5 — dashboard "Add widget" gallery (FR-99.2, safer half), GREEN.** The edit-mode add-widget row of bare pills became a **gallery of cards** — each available widget shows its name + a description (`dashboard.widgetDesc.*`, trilingual, all 13 widgets) + an add affordance, with hover elevation. Reorder/resize stay on the existing per-widget **keyboard-accessible** arrow/size controls (FR-99.2 accepts these as the keyboard path); the risky pointer-drag reorder is deferred to a browser-verified pass. `?widgets=`/`dashboard_layout` contract untouched; the FR-90.3 layout test passes unchanged.
- **Remaining (SP2 tail → SP5):** the subscriber-form ordering pass (FR-98.2, the dense form — deferred for a browser-verified pass), and pattern adoption across billing/payments/vouchers/reports (SP2), NAS/pools/live/devices/health/alerts (SP3), security/settings + the "My account" area (SP4, FR-99.1), portal + dashboard direct-manipulation (SP5, FR-99.2). Each is an independent, per-screen, keep-suite-green increment on the now-shipped foundation.

## Documented-pending
The live-browser visual matrix is the human half of the gate (no browser in-session). Standing unrelated: `cmd/hikrad-updaterd`'s two rollback tests (pre-existing).
