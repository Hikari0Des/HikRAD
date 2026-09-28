# Phase v3-1 — Full UI/UX Modernization Program — Gate Result (INTERIM)

**Verdict: GREEN for everything shipped; phase INTERIM (not closed).** FR-97 (foundation) is complete; FR-98/FR-99 are partially delivered. The remaining screen sweep + the pointer-drag reorder are **deferred to a browser-verified pass** — this is the documented-pending *visual* leg every frontend phase carries (no browser in-session), made explicit here because v3-1 is inherently visual and the owner's bar is "must not look AI-built," which only a human eye at a real screen can sign off.

FR-97/98/99 (PRD Decision 53), sub-PRD 07. **No migration.** In-place progressive redesign; behavior-preserving; the app is shippable at this state.

## What shipped (GREEN, verified)

| Sub-phase | Delivered | Key files |
|---|---|---|
| **SP1 — FR-97 foundation** | Token layer (radius pinned to Tailwind defaults → zero existing-screen shift; elevation refined + dark-mode invisible-shadow fixed, both apps); shared primitives `Card`/`StatCard`/`SectionCard`/`DataTable` (`@hikrad/shared/ui`, `.hk-*` CSS); panel patterns `FilterBar`, `FormSection`+`FormField` (label-associated **by construction**), `DetailHeader`; `design-language.md` + dev-only `/dev/design` gallery **and** a dev-only public `/design` route (backend-free review) | `theme/tokens.css`, `tailwind.config.ts` (both apps), `shared/src/ui/{Card,DataTable,states}.tsx` + `ui.css`, `components/patterns/*`, `pages/DesignGalleryPage.tsx`, `design-language.md` |
| **SP2 — FR-98.1/2** | Subscribers list row-of-selects → chip **FilterBar** (new `q` search wired to both client filter and server bulk filter; bulk parity + per-manager persistence preserved); profile create/edit form **regrouped** (Identity & billing → Speed & limits → Advanced-collapsed, auto-expanding when editing a plan with advanced values set) | `pages/subscribers/UserListPage.tsx`, `pages/profiles/ProfilesPage.tsx` |
| **SP3 — pattern adoption** | NAS list + Pools cards standardized onto the shared `Card` primitive (design-system elevation) | `pages/nas/NasListPage.tsx`, `pages/pools/PoolsPage.tsx` |
| **SP4 — FR-99.1** | Profile-centric **"My account"** area (`/me` landing: My security / preferences / payment methods + admin quick-links as `Card` links; user-menu entry). Existing routes unchanged and still resolve | `pages/MyAccountPage.tsx`, `App.tsx`, `shell/UserMenu.tsx` |
| **SP5 — FR-99.2 (safer half)** | Dashboard edit-mode **Add-widget gallery** (name + trilingual description + add affordance per widget, all 13) replacing the bare-pill row; reorder/resize stay on the existing keyboard-accessible controls; `dashboard_layout` contract untouched | `pages/DashboardPage.tsx`, `dashboard/widgetDesc.*` i18n |

## Gate items
1. **FR-97 foundation — PASS.** `/design` + `/dev/design` render every pattern; shared `patterns.test.tsx` (Card/StatCard/SectionCard/DataTable incl. loading/empty/error) + panel `FilterBar.test.tsx` green; `FormField` associates its label by construction (render-prop id).
2. **FR-98.1 subscribers FilterBar — PASS.** Chips render/remove, per-manager persistence preserved, **bulk match-set unchanged** (the `q` mirror to the server filter keeps bulk parity); subscribers suite green unmodified.
3. **FR-98.2 profile form-ordering — PASS.** Grouped into `FormSection`s; Advanced collapsed by default; every field preserved.
4. **FR-99.1 account area — PASS.** `/me` reachable from the user menu in ≤2 clicks; all sub-routes still resolve (nothing moved).
5. **FR-99.2 add-widget — PASS.** Gallery with descriptions; `?widgets=`/`dashboard_layout` unchanged; the FR-90.3 layout test passes unchanged.
6. **CI floors — PASS throughout.** `lint-no-native-controls.sh`, `layoutSmoke`, stylelint logical-properties, `i18n:check` (1647 keys × 3 locales) all green. **The frontend gate now runs `npm run build`** (not just `tsc --noEmit`) after this phase surfaced 3 test-file-only type errors + a bundler-resolution error that the looser check missed (known-issues 2026-07-24).
7. **No regressions — PASS.** Backend `go build`/`vet` clean; **panel 36 files / 121 tests + shared 60 tests** green; panel + portal `npm run build` + lint clean.

## Deferred to a browser-verified pass (documented-pending)
- **The remaining screen adoption** across billing/payments/vouchers/reports, NAS detail/monitoring/security/settings, and the portal — mechanical but visual; safer with the app on screen.
- **Pointer-drag reorder** for the dashboard (the keyboard arrows already meet the a11y bar; drag needs to be *seen*).
- **FilterBar on the live-sessions page** — its filters drive an SSE re-subscription; refactoring blind risks the stream.
- **The dense subscriber-form** ordering pass (many conditional sections).
- **The live-browser visual matrix** — 360/768/1280 × light/dark × LTR/RTL — the standing human leg from v2-12, now the sign-off for the redesigned screens.

## Design decisions worth recording
- **Radius tokens == Tailwind defaults** so tokenizing the scale shifted no existing screen; only elevation was intentionally refined (and the dark-mode shadow, a real prior bug, fixed).
- **Shared primitives use `.hk-*` CSS, not Tailwind utilities** — the panel's Tailwind doesn't scan `@hikrad/shared` sources, so utility classes there wouldn't generate; the primitives must be self-contained CSS on the `--hik-*` tokens.
- **Label-association delivered "by construction"** via `FormField`'s generated-id render-prop rather than editing 35 bare `Field` call sites at once — new/redesigned forms get it for free; the bare sites retire as their screens are swept.

## Standing unrelated
`cmd/hikrad-updaterd`'s two rollback tests still fail (pre-existing, confirmed against a clean tree during v3-3).
