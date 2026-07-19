# v3-1 — Full UI/UX Modernization Program

> Owner request 2026-07-19 (review items 1, 2, 4, 6, 11, 15; PRD Decision 47a). Proposed FR numbers: **FR-97 (design system), FR-98 (screen overhaul), FR-99 (account area + dashboard UX)** — committed to the master PRD at kickoff, not before. Owner: sub-PRD 07 (same cross-cutting precedent as FR-94–96).

## 1. Problem

v2-12 modernized the *controls* (Radix everywhere, zero native chrome) but not the *product*: screens still carry v1's layout decisions — filter bars that are rows of bare inputs (owner: "subscribers filters feels like trash"), forms whose field order grew by accretion rather than by task (item 6), a dashboard customization flow that works but "feels complicated" (item 2), and settings/security/preferences scattered across the sidebar instead of gathered around the person using them (item 15). The owner wants the whole UI rebuilt to be "extreme usable and easy and simple but packs features and modernized with no issues or bugs while also keeping in mind to never make it seems ai built" (item 11).

**Chosen approach (owner decision 2026-07-19): in-place progressive redesign.** No parallel frontend, no framework change. The app must remain shippable after every sub-phase; all backend contracts, i18n/RTL rules, and the existing test suites survive throughout.

## 2. Scope (what ships)

### FR-97 — Design system foundation
- A documented token layer (spacing scale, type scale, radii, elevation, semantic colors for both themes) replacing ad-hoc Tailwind values; existing `data-theme` tokens are the base, extended not replaced.
- A pattern library of composed components on top of the v2-12 control set: page shell (header/actions/breadcrumb), data table (sticky header, row density, empty/loading/error states), **FilterBar** (see FR-98), form layout primitives (sections, field groups, two-column collapse), detail-page header, stat/tile cards, confirmation and side-panel patterns.
- A written **design language doc** (`docs/v3/phases/phase-v3-1-*/design-language.md`): voice, density, motion (reduced-motion respecting), when to use which pattern — the "doesn't look AI-built" guard is a human-taste checklist here, enforced at review: no gradient-soup, no emoji-in-UI, no fake testimonial energy; typography and spacing consistency; real empty states written in product voice (trilingual).
- Storybook-style gallery route (dev-only) or a `/design` smoke page so every pattern is visually reviewable in both themes × 3 locales at once.

### FR-98 — Screen-by-screen overhaul
- **Subscribers list first** (item 4's direct target): FilterBar pattern = one search field + typed filter chips (add-a-filter menu → status/profile/owner/service/expiring pickers), active chips removable inline, persisted per manager (the 2026-07-19 `usePersistentState` mechanism), server round-trip unchanged. Saved-view pills if cheap; otherwise defer.
- **Form ordering pass** (item 6): every create/edit form re-grouped by task frequency — identity → service → billing → advanced, progressive disclosure for rarely-used fields (advanced sections collapsed by default). Explicit per-form field-order spec goes in the phase brief before any code, so it's reviewable.
- Every remaining panel screen and the portal's screens adopt the FR-97 patterns. Order and batching are fixed in the execution plan (roughly: subscribers → billing/payments → NAS/monitoring → security/settings → portal).
- Behavior-preserving: no endpoint, permission, or i18n-key semantics change without an explicitly amended contract.

### FR-99 — Account area + dashboard customization UX
- **Profile-centric account area** (item 15): one "My account" section (off the user menu) gathering My security (2FA/sessions), My preferences, My payment methods, and — for admins — quick links into the administration settings that concern the signed-in person. Existing routes keep working (redirects), permission gating unchanged.
- **Dashboard customization simplification** (item 2's UX half): replace the current edit-mode chip/arrow flow with direct manipulation — drag to reorder (keyboard accessible fallback = existing arrows), a single "Add widget" gallery with previews and per-widget descriptions, resize as a visible handle/toggle on the card. Server contract (`?widgets=`, `dashboard_layout` in preferences) unchanged.

## 3. Non-goals
- No new backend features; no framework/library migration; no portal/panel merge.
- No rebranding (FR-91–93 stand as-is); the fixed "Powered by HikRAD" mark stays.
- Native mobile apps, TWA wrappers: still candidate-backlog.

## 4. Constraints carried forward
- Trilingual + true RTL on every touched screen (`npm run i18n:check` fatal), LTR islands preserved; logical properties only (stylelint gate).
- The v2-12 CI gates (`lint-no-native-controls.sh`, `layoutSmoke.test.tsx`) must stay green throughout — the redesign builds *on* the control set, never around it.
- Accessibility must improve, not just hold: the long-open `Field` label-association gap (known-issues 2026-07-18 row) is **in scope** — the FR-97 form primitives associate labels programmatically by construction, and the FR-98 sweep retires the ~35 remaining bare call sites.
- Phone-first: every redesigned screen is specified at 360px before desktop.

## 5. Acceptance sketch (details frozen in the phase briefs)
- Design-language doc + pattern gallery exist and render both themes × en/ar/ku.
- Subscribers FilterBar: a filter set survives navigation and re-login (per manager), chips removable, 0 regressions in list/bulk semantics (existing tests pass unmodified or with reviewed updates).
- Every form's field order matches the spec committed in the brief; advanced fields are collapsed by default and state is remembered.
- Dashboard: add/reorder/resize each ≤ 2 interactions from edit mode; layout round-trips through preferences unchanged.
- Account area reachable in ≤ 2 clicks from anywhere; old routes redirect.
- Full suites green after every sub-phase: backend untouched-green, `frontend` lint/build/test/i18n:check, both apps' layout smoke.
- A live-browser visual matrix (360/768/1280 × light/dark × LTR/RTL) — the same leg v2-12 left documented-pending — is the gate's human half.

## 6. Open questions (resolve at kickoff, not before)
1. Sub-phase count: 3 (foundation / panel screens / portal+account) vs 5 (foundation / subscribers+billing / NAS+monitoring / security+settings+account / portal). Recommend 5 — smaller shippable slices.
2. Drag-and-drop dependency for the dashboard (dnd-kit?) vs pointer-events hand-roll. Decide in the brief; whichever is chosen must pass the keyboard-accessibility bar.
3. Whether saved filter *views* (named filter sets) make v3-1 or fall to backlog.

---

## AI kickoff prompt (paste into a fresh session to start v3-1)

```
Read CLAUDE.md, docs/PRD.md §Decisions (43–47), docs/v3/00-v3-index.md, and docs/v3/01-ui-ux-modernization.md, then run the standing v2 kickoff protocol (docs/v2/phases/00-v2-execution-plan.md, adapted to v3 paths) for phase v3-1:

1. Amend the master PRD: commit FR-97/98/99 exactly as scoped in the brief (or amend the brief FIRST if research contradicts it — never silently), add a Decision row, update sub-PRD 07.
2. Research pass BEFORE freezing contracts: inventory every panel/portal screen and every <Field> call site; produce the per-form field-order spec and the screen batching for FR-98; resolve the brief's §6 open questions with the owner via AskUserQuestion.
3. Write docs/v3/phases/phase-v3-1-ui-modernization/00-phase.md with frozen contracts: the token layer, each pattern component's API, the FilterBar contract, the account-area routes/redirects, the dashboard interaction spec, sub-phase boundaries, and the integration gate (including the live-browser matrix leg and the Field-association sweep).
4. Present the phase brief and STOP for owner confirmation before writing feature code.
5. Execute sub-phases sequentially, committing in reviewable chunks; keep all CI gates green after each; record every bug found in docs/ops/known-issues.md; finish with gate-result.md.

Constraints you may not relax: in-place progressive redesign (no parallel frontend), behavior-preserving, trilingual+RTL on everything, v2-12's CI gates stay green, migrations linear-next-number (no migration expected for this phase), SOLO sequential execution.
```
