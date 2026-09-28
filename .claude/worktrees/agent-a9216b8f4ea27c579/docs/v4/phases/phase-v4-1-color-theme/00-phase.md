# Phase v4-1 — Color & theme system rebuild

> **FRs:** FR-115, FR-116, FR-117 · **Migrations:** none · **Depends on:** v4-0 · **Blocks:** every screen phase (v4-5…v4-12)
> **Owner sub-PRD:** 07 (frontend) · **Decisions committed at kickoff:** none new

## 1. The actual diagnosis

"The coloring is fucked" is not a taste complaint — it is four concrete, measurable defects. All four were verified against the working tree on 2026-08-16.

**D1 — The two apps have divergent token sets.** `frontend/panel/src/theme/tokens.css` defines `--hik-warn`. `frontend/portal/src/theme/tokens.css` defines `--hik-warning`. Same semantic role, different names, two separate files. Anything moved between the apps — or placed in `@hikrad/shared` — silently loses its color.

**D2 — There is a live undefined-token bug in the panel/shared direction of that divergence.** Five portal files use `border-warning` / `bg-warning` (`PayPanel.tsx` ×2, `HomePage.tsx`, `SettingsPage.tsx`, `OfflineBanner.tsx`). This is the *exact* class of bug FR-104 already fixed once for `bg-warn`, reintroduced from the other side. An undefined Tailwind color emits no rule, so the element renders unstyled — a warning banner with no warning color, and an offline banner with no background at all.

**D3 — There is no border color token.** `--hik-border` does not exist. **161 call sites** use `border-surface-sunken` as a stand-in, plus 23 more at `/60` opacity. A *surface* token is being used as a *line* token, which is why borders read as heavy blocks in light mode and nearly vanish in dark (dark `surface-sunken` is lighter than `surface`, so the relationship inverts).

**D4 — The token set is too thin to express a UI.** Three surfaces, three inks, three semantic colors, and nothing else. Missing: focus ring, hover/active/selected state layers, `info`, disabled, and any data-series palette for charts. Every component therefore improvises with opacity modifiers on tokens that were never meant to carry state.

**D5 — Dark-mode users get a light first paint.** `tokens.css` has no `prefers-color-scheme` block; the comment states `system` is resolved in JS by the shared theme store, which sets `data-theme` after hydration. Until then, `:root` light values apply.

Nothing here is a discipline failure: a grep for raw Tailwind palette utilities across all 209 `.tsx` files returns **zero**. The components are disciplined; the token layer they are disciplined *against* is incomplete and duplicated.

## 2. Scope

**In:** the token layer, its CI gates, and theme boot. Component changes only where a token rename or a new role requires them.
**Not in:** layout, spacing, typography, component restructuring, or any screen redesign. v3-1's in-progress screen sweep continues on top of this, afterwards.

## 3. Work

### 3.1 FR-115 — One token contract
- Move the canonical definitions to `@hikrad/shared` (`src/theme/tokens.css`), imported by both apps. The per-app files become imports, not definitions.
- **Resolve the divergence to one name.** `warn` and `warning` collapse into `warn` (the panel's name, and the shorter one); the five portal call sites are updated in the same commit.
- Add the missing roles, all as RGB triplets so alpha composition keeps working:
  - `--hik-border`, `--hik-border-strong`
  - `--hik-focus` (focus ring, must satisfy 3:1 against both adjacent surfaces)
  - `--hik-state-hover`, `--hik-state-active`, `--hik-state-selected` (overlay layers, not solid fills)
  - `--hik-info` (the fourth semantic color; today `info` is faked with `brand`)
  - `--hik-disabled`, `--hik-disabled-ink`
  - `--hik-chart-1` … `--hik-chart-8` — a categorical series palette that is distinguishable in both themes and under the common color-vision deficiencies
- Extend both Tailwind configs to expose every new role, keeping the existing key names byte-identical so no current utility shifts.
- Migrate the 161 + 23 `border-surface-sunken` call sites to `border-border`. Mechanical, but it is the change that makes dark mode look built rather than inverted.

### 3.2 FR-116 — Gates
Three new checks, all CI-fatal, all wired into `npm run lint` for both apps:
1. **`lint-tokens.sh`** — extracts every `--hik-*` reference and every `bg-|text-|border-|ring-` utility resolving to a token, and fails if any is undefined in **either** app. This would have caught D2 on the commit that introduced it.
2. **`lint-no-raw-palette.sh`** — fails on any raw Tailwind palette utility outside the token layer. Currently passing; this locks it in.
3. **`contrast.test.ts`** — asserts WCAG AA for every foreground/background token pair the design language declares legal, **in both themes**, computed from the token values themselves. Not a screenshot test, not a human judgement.

### 3.3 FR-117 — No-flash boot
- Add a `@media (prefers-color-scheme: dark)` block redefining the tokens, guarded so an explicit `data-theme="light"` still wins.
- Add a minimal pre-hydration inline script in both `index.html` files that stamps `data-theme` from stored preference before first paint.
- Result: three states resolve correctly — explicit light, explicit dark, and system — with no flash in any of them.

### 3.4 Design language doc
Update `design-language.md` with the full role table, the legal pairings the contrast test enforces, and the rule that a component may reference **only** role tokens — never a raw color, never a surface token as a line, never opacity as a substitute for a state layer.

## 4. Frozen contracts

- **Token names are a contract.** The role list in §3.1 is frozen for v4; adding a role is an amendment in a later brief. Renaming one requires updating both apps and the design doc in a single commit.
- Tokens are defined **once**, in `@hikrad/shared`. A per-app `:root` block redefining a role is a lint failure.
- Components reference roles through Tailwind utilities; raw `var(--hik-*)` in a component is permitted only inside `@hikrad/shared/ui`'s own CSS.

## 5. Acceptance gate

| # | Item | Proof |
|---|---|---|
| 1 | One definition site | `grep -rn '^\s*--hik-' frontend --include=*.css` shows definitions only in `shared` |
| 2 | Divergence gone | No `--hik-warning` anywhere; all five portal call sites use `warn` |
| 3 | No undefined tokens | `lint-tokens.sh` green in both apps; deliberately introduce `bg-nonexistent` and confirm it fails |
| 4 | Border role adopted | Zero `border-surface-sunken` remain; `border-border` count ≈ 184 |
| 5 | Contrast AA both themes | `contrast.test.ts` green; output table pasted into the gate result |
| 6 | No raw palette | `lint-no-raw-palette.sh` green |
| 7 | No flash | Set OS to dark with no stored preference, hard-reload both apps, record first paint is dark |
| 8 | Nothing shifted | Full build green in both apps; existing component tests unmodified and passing |
| 9 | **Human visual pass** | 360/768/1280 × light/dark × LTR/RTL in a real browser, both apps, screenshots attached. **Declared at kickoff as a human leg — it may not be scripted away** |

## 6. Commit plan

1. `feat(theme): move canonical tokens to @hikrad/shared (FR-115)`
2. `fix(theme): collapse --hik-warning into --hik-warn, fix 5 undefined-token call sites (FR-115)`
3. `feat(theme): add border/focus/state/info/disabled/chart roles (FR-115)`
4. `refactor(theme): migrate 184 border-surface-sunken call sites to border-border (FR-115)`
5. `ci(theme): add undefined-token and raw-palette lint gates (FR-116)`
6. `test(theme): assert WCAG AA on every legal token pair in both themes (FR-116)`
7. `fix(theme): resolve system theme before first paint (FR-117)`
8. `docs(theme): design-language role table and pairing rules (FR-115)`
9. `docs: v4-1 gate result`

## 7. Kickoff prompt

```
Execute phase v4-1 (Color & theme system rebuild) of the HikRAD v4 program, solo and
sequentially, start to finish, in this repo.

Read first:
  - docs/v4/00-v4-master-plan.md
  - docs/v4/phases/phase-v4-1-color-theme/00-phase.md   (this brief — §1 is the diagnosis)
  - frontend/panel/src/theme/tokens.css and frontend/portal/src/theme/tokens.css
  - frontend/panel/tailwind.config.ts and frontend/portal/tailwind.config.ts
  - frontend/panel/src/design-language.md

Deliver FR-115, FR-116, FR-117.

Verify the diagnosis before you change anything — all five defects were confirmed on
2026-08-16 and you should reconfirm, because if one has drifted the fix changes:
  D1 panel defines --hik-warn, portal defines --hik-warning — two token sets.
  D2 five portal files use border-warning/bg-warning, which resolve to NOTHING.
  D3 no --hik-border exists; 161 + 23 call sites abuse border-surface-sunken as a line color.
  D4 no focus/hover/active/selected/info/disabled/chart roles exist at all.
  D5 no prefers-color-scheme fallback — dark-mode users get a light first paint.

The components are NOT the problem: a raw-Tailwind-palette grep across all 209 .tsx files
returns zero. Fix the token layer, not the components.

Hard constraints:
  - Tokens are defined ONCE, in @hikrad/shared. Per-app files import, never redefine.
  - Existing token key names stay byte-identical so no current utility shifts. Radius values
    must keep matching Tailwind's defaults — the whole point of v3-1's foundation was that
    zero existing screens move.
  - No layout, spacing, typography, or component restructuring. Tokens only.
  - The contrast test computes from token values programmatically. Do not substitute a
    screenshot or a judgement call.
  - Prove gate item 3 by deliberately introducing a bad token and watching CI fail, then
    reverting. A gate that has never failed has not been tested.

Gate item 9 is a HUMAN browser leg (360/768/1280 × light/dark × LTR/RTL, both apps). It is
declared here at kickoff, per the program's rule that such legs are named up front. If no
browser is available in your session, say so in the gate result and mark the phase gate
INCOMPLETE — do not mark it GREEN with the leg pending.

Finish with docs/history/gates/v4-1.md.
```
