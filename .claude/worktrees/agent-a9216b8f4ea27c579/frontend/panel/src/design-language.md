# HikRAD design language

**Scope of this revision (v4-1, FR-115/116/117):** the color/theme token
layer — role names, values, legal pairings, and the boot sequence. The
voice/density/motion/pattern-gallery material FR-97.3 originally put here was
lost when v4-0's docs collapse deleted `docs/v3/phase-v3-1-*/design-language.md`
(its content folded into `docs/history.md`'s v3-1 row, not into this file,
which did not exist at `frontend/panel/src/` before this phase). This
revision does not attempt to reconstruct that material from memory — it
covers only what v4-1 actually built: the token contract. Later screen-sweep
phases (v4-5…v4-12) are expected to extend this file with pattern/voice/
motion sections as they do that work, the way FR-97.3 originally intended.

This file documents `@hikrad/shared/src/theme/tokens.css` — the **one**
canonical token definition site (FR-115 frozen contract). It is not
duplicated per app; `frontend/panel/src/theme/tokens.css` and
`frontend/portal/src/theme/tokens.css` are both one-line imports of it.

## The rule

A component may reference **only** a role token, through a Tailwind utility
(`bg-surface`, `text-ink-muted`, `border-border`, …). Specifically forbidden:

- **A raw color** — no `bg-red-500`, no inline `style={{ color: '#...' }}`.
  Enforced by `scripts/lint-no-raw-palette.sh` (FR-116).
- **A surface token used as a line** — this was D3, the exact bug this phase
  fixed (161 + 23 call sites had used `border-surface-sunken` as a border
  color because no border role existed). Use `border-border` /
  `border-border-strong`.
- **Opacity as a substitute for a state layer** — hover/active/selected are
  `bg-state-hover/N`, `bg-state-active/N`, `bg-state-selected/N`, not an
  ad-hoc `hover:opacity-80` on the element itself.
- **`var(--hik-*)` directly** — permitted only inside `@hikrad/shared`'s own
  CSS (`src/ui/ui.css`), because that package's Tailwind content globs don't
  reach every consumer. Everywhere else, go through the Tailwind utility.

`scripts/lint-tokens.sh` (FR-116) fails CI if any of the above is referenced
but doesn't actually resolve to a rule for a given app — the exact failure
mode of D2 (`bg-warning`/`border-warning` in 5 portal files, silently
unstyled) and of the `bg-success`/`text-success` call site this phase also
found and fixed in `SystemSettings.tsx` (same bug class, not previously
known).

## Role table

RGB triplets (so Tailwind can compose alpha via `<alpha-value>`). "Utility"
is the Tailwind class family each role generates.

| Role                                                            | Utility                                                        | Light                                                     | Dark                                                              | Purpose                                                                                                                                                                                                                                                                                                                                                                           |
| --------------------------------------------------------------- | -------------------------------------------------------------- | --------------------------------------------------------- | ----------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `brand` / `brand-strong` / `brand-soft`                         | `bg-brand`, `bg-brand-strong`, `bg-brand-soft`                 | `8 116 143` / `6 88 108` / `224 242 246`                  | `45 156 186` / `122 199 220` / `21 51 61`                         | ISP-brandable primary color                                                                                                                                                                                                                                                                                                                                                       |
| `surface` / `surface-raised` / `surface-sunken`                 | `bg-surface`, `bg-surface-raised`, `bg-surface-sunken`         | `244 246 248` / `255 255 255` / `232 236 240`             | `16 22 29` / `25 33 43` / `40 51 63`                              | Page / card / recessed fills                                                                                                                                                                                                                                                                                                                                                      |
| `ink` / `ink-muted` / `ink-inverse`                             | `text-ink`, `text-ink-muted`, `text-ink-inverse`               | `23 32 42` / `100 112 126` / `255 255 255`                | `226 233 240` / `148 160 174` / `13 18 24`                        | Primary / secondary / on-brand text                                                                                                                                                                                                                                                                                                                                               |
| `danger`                                                        | `bg-danger`, `text-danger`                                     | `185 28 28`                                               | `240 106 106`                                                     | Destructive / error                                                                                                                                                                                                                                                                                                                                                               |
| `warn`                                                          | `bg-warn`, `text-warn`                                         | `180 83 9`                                                | `245 176 80`                                                      | Caution, expiring, grace period. **Collapsed from the panel's `warn` / portal's `warning` divergence (D1) — `warn` is the only name; `warning` does not exist anywhere in either app.**                                                                                                                                                                                           |
| `ok`                                                            | `bg-ok`, `text-ok`                                             | `21 128 61`                                               | `84 205 132`                                                      | Success, active, healthy                                                                                                                                                                                                                                                                                                                                                          |
| `info` **(new, D4)**                                            | `bg-info`, `text-info`                                         | `29 91 199`                                               | `122 176 250`                                                     | Neutral/informational — previously faked with `brand`                                                                                                                                                                                                                                                                                                                             |
| `border` / `border-strong` **(new, D3)**                        | `border-border`, `border-border-strong`                        | `199 207 215` / `130 142 155`                             | `54 66 80` / `108 124 142`                                        | The line role. `border` is a subtle decorative divider (not AA-asserted — same WCAG exemption ordinary Tailwind gray borders get); `border-strong` is the emphasized divider and **is** AA-asserted (≥3:1) since it needs to read as a real boundary.                                                                                                                             |
| `focus` **(new, D4)**                                           | `ring-focus`                                                   | `29 91 199`                                               | `122 176 250`                                                     | Focus ring. Deliberately the same value as `info` — one accent hue for "notable/interactive" across focus rings, links, and info banners. AA-asserted ≥3:1.                                                                                                                                                                                                                       |
| `state-hover` / `state-active` / `state-selected` **(new, D4)** | `bg-state-hover/N`, `bg-state-active/N`, `bg-state-selected/N` | ink / ink / brand                                         | ink / ink / brand                                                 | Translucent overlay layers, never solid fills. hover/active share the neutral ink scrim, differentiated by the alpha the call site applies (hover ~6%, active ~12%); selected uses the brand tint (~10–14%) so a persisted selection reads as distinct from a transient interaction. Not AA-asserted — they're overlays composited onto whatever is underneath, not a fixed pair. |
| `disabled` / `disabled-ink` **(new, D4)**                       | `bg-disabled`, `text-disabled-ink`                             | `226 230 234` / `150 160 172`                             | `33 41 51` / `92 103 117`                                         | Disabled fill/ink. **Not AA-asserted** — WCAG explicitly exempts disabled controls from contrast requirements.                                                                                                                                                                                                                                                                    |
| `chart-1` … `chart-8` **(new, D4)**                             | `bg-chart-N`, `text-chart-N`, `fill-chart-N`, …                | Okabe–Ito derived, 8th ("black") swapped for neutral gray | Brightness-lifted variants of the same hues, for dark surfaces    | Categorical data-series palette, distinguishable under common color-vision deficiencies. Graphical marks, not text — not part of the AA contrast table; legibility on real charts is a human visual-pass item (gate item 9), not a computed ratio.                                                                                                                                |
| `radius-sm/md/lg/xl`, `shadow-sm/md/lg`                         | `rounded-*`, `shadow-*`                                        | Radius: Tailwind's own defaults, unchanged since v3-1     | Shadow: deeper/softer on dark surfaces (the v3-1 dark-shadow fix) | Unrelated to color; carried over unchanged by this phase (frozen — radius intentionally equals Tailwind's own scale so no existing `rounded-*` utility shifted, ever, including in this phase).                                                                                                                                                                                   |

## Legal pairings (what `contrast.test.ts` asserts)

Computed from the actual token values in `tokens.css` — not hand-checked,
not a screenshot. Every pair below clears its bar in **light**, **dark**,
and the **`prefers-color-scheme` dark fallback** (which is asserted to
literally equal the `dark` block, so it can never silently drift).

**Text pairs — WCAG AA, ≥4.5:1:**

`ink`/`surface`, `ink`/`surface-raised`, `ink`/`surface-sunken`,
`ink-muted`/`surface`, `ink-muted`/`surface-raised`,
`ink-inverse`/`brand`, `ink-inverse`/`brand-strong`, `ink-inverse`/`danger`,
`ink-inverse`/`warn`, `ink-inverse`/`ok`, `ink-inverse`/`info`,
`danger`/`surface`, `danger`/`surface-raised`,
`warn`/`surface`, `warn`/`surface-raised`,
`ok`/`surface`, `ok`/`surface-raised`,
`info`/`surface`, `info`/`surface-raised`,
`brand`/`surface`, `brand`/`surface-raised`.

**UI-component pairs — WCAG AA, ≥3:1:**

`focus`/`surface`, `focus`/`surface-raised`,
`border-strong`/`surface`, `border-strong`/`surface-raised`.

**Deliberately not asserted:** `border` (plain — decorative divider,
exempt), `disabled`/`disabled-ink` (WCAG-exempt), `chart-1`…`chart-8`
(graphical marks, not text — see the role table).

## Boot sequence (FR-117, no flash)

Three preference states, three different mechanisms, zero flash in any of
them:

1. **Explicit `light`/`dark` stored** — a synchronous inline script, first
   thing in `<head>` in both `index.html` files, reads `localStorage`
   (`hikrad.theme`) and stamps `data-theme` before any stylesheet is even
   linked. `initTheme()` re-applies the same value post-hydration
   (idempotent, not a race).
2. **`system`, OS is dark** — no stored value for the inline script to read,
   so `tokens.css`'s `@media (prefers-color-scheme: dark)` block resolves
   the dark palette immediately, with zero JS involved. Guarded with
   `:not([data-theme='light'])` so an explicit light override always wins
   even on a dark OS.
3. **`system`, OS is light** — `:root`'s defaults are already light; nothing
   to do.

## Amending this file

Token names are frozen for v4 (`docs/v4/phases/phase-v4-1-color-theme/00-phase.md`
§4) — adding a role is a v4 amendment in a later phase brief; renaming one
requires updating both apps' Tailwind configs, `tokens.css`, and this file in
the same commit.
