# v2-12 → phase v2-10 — Frontend modernization pass (runs LAST)

> Owner request 2026-07-17 (ex-v3.1, merged into v2 by PRD Decision 32, scope broadened the same day): "improve the frontend, make it more responsive, remove its bugs — like the legacy scrollbar showing when the web can't fit the screen — and **completely fix legacy scrollbars and dropdowns and fields and ticks etc.** to make them modernized." This phase deliberately runs **after every other UI-touching v2 phase** so nothing un-modernizes it afterwards.

## 1. Problem

1. **Legacy-looking native controls.** Dropdowns/`<select>`s, text fields, checkboxes ("ticks"), radios, file inputs, and date/number inputs largely render as browser-native chrome — visually inconsistent with the Tailwind/Radix design and between browsers/OSes. Scrollbar *styling* was patched in v1.x (2026-07-17, slim + theme-colored), but the rest of the control set was not.
2. **Overflow/responsiveness bugs.** Screens can overflow the viewport and scroll the body horizontally (the original "legacy scrollbar when the web can't fit the screen" report) instead of wrapping or scrolling inside their own containers. No repo-wide responsive audit has ever been done.
3. **Polish debt.** Inconsistent spacing/empty states/loading states/transitions accumulated across five v1 phases and the v2 phases before this one.

## 2. Requirements (draft — renumber at kickoff)

### FR-A — Modern control system (complete, both apps)
- One styled, accessible control set used **everywhere** in panel + portal: Select/dropdown (Radix-based, styled options — no native popup), TextInput/number/date, Checkbox + Radio + Switch with custom ticks, file upload, combobox where lists are long (NAS/profile pickers). Extend `frontend/panel/src/components/form/` and mirror what portal needs via `@hikrad/shared`.
- Zero remaining native-chrome form controls: a repo grep/lint for bare `<select>`/`<input type="checkbox">` outside the component library gates CI.
- Keyboard + screen-reader behavior preserved (Radix primitives); RTL correct; usernames/MACs/IPs stay LTR islands.

### FR-B — Responsive/overflow audit
- Every screen audited at 360px / 768px / 1280px: the **body never scrolls horizontally** — wide content (tables, charts, code/snippet blocks) scrolls inside its own `overflow-x-auto` container; forms wrap; modals fit small screens.
- An automated guard where cheap (vitest + jsdom layout smoke or a documented manual matrix in the gate).

### FR-C — Polish sweep
- Consistent spacing scale, empty states, loading skeletons, focus rings, and motion (reduced-motion respected) across both apps; dark/light parity checked per screen.

## 3. Impact map

Touches `frontend/panel/**`, `frontend/portal/**`, `frontend/shared/**` broadly (that is the point); no backend changes expected; no migrations.

## 4. Acceptance sketch

- On a 360px phone, every panel + portal screen is usable with no body horizontal scrollbar anywhere; tables scroll inside their cards.
- Every dropdown, field, tick, radio and switch in both apps renders the styled control set in Chrome and Firefox, light and dark, LTR and RTL — no native chrome anywhere; CI fails if a bare native control sneaks in.
- i18n check still green; existing tests still pass; no user-visible string changes without locale updates.

## 5. AI kickoff prompt (paste into a fresh Claude Code session at repo root)

```text
You are working in the HikRAD repo. We are starting v2 phase 10 — the LAST v2 phase: the frontend modernization pass (PRD Decision 32). Confirm every other v2 phase is complete before proceeding. You work SOLO — no parallel agents; execute sequentially (shared control set → panel adoption → portal adoption → responsive audit → polish sweep), committing in reviewable chunks per screen-group so regressions bisect cleanly.

Read, in this order and nothing else yet: CLAUDE.md, docs/v2/phases/00-v2-execution-plan.md, docs/v2/12-frontend-modernization.md, frontend/panel/src/components/form/, frontend/shared/src/ui/, and docs/ops/known-issues.md (the 2026-07-17 layout row).

Step 1 — Amend the docs (single commit): FR rows + Decisions Log row in docs/PRD.md, update sub-PRD 07 + any UI-owning sub-PRDs, docs/prd/00-index.md.

Step 2 — Create docs/v2/phases/phase-v2-10-frontend-modernization/00-phase.md with frozen contracts (the control-set component API, the no-native-controls lint rule, the responsive matrix) and the integration gate (grep-gate green, 360px matrix, RTL + dark parity spot-checks, all existing suites green; no migrations). Scriptable items → scripts/gate-v2-phase-10.sh.

Step 3 — Stop and present the phase brief for my confirmation before writing feature code.

Constraints: behavior-preserving — no feature changes ride along; accessibility and RTL must not regress (Radix primitives, logical CSS properties only); LTR islands stay LTR; every user-visible string trilingual; update every doc invalidated; record bugs in docs/ops/known-issues.md (and close its open layout row).
```
