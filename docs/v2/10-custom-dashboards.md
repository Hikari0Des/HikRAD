# v2-10 — Customizable per-manager dashboards

> Owner request 2026-07-17 (ex-v3.2, merged into v2 by PRD Decision 32): "adjustable and customizable dashboard per manager."

## 1. Problem

The FR-32 dashboard is one fixed layout for everyone. Omar (owner) wants revenue and NAS health first; Sara (front desk) cares about expiring subscribers; Hassan (agent) mostly needs his balance and his own subscribers. Nobody can reorder, hide, or resize anything, and a manager without `reports.view` still gets the layout designed around tiles they can't load.

## 2. Requirements (draft — renumber at kickoff)

### FR-A — Widget catalog
- Every existing dashboard tile becomes a catalog widget with a stable id, a permission requirement, and a size class: online-now (+sparkline), revenue-today, RADIUS rps, subs active/expired/expiring, pipeline health, NAS health cards, plus cheap new ones surfaced by later phases (my-balance for agents, pending payment-review count once v2-2 lands, alerts feed).
- A widget a manager lacks permission for is **not offered** in the picker and never rendered (same deny-by-default string-permission checks as everywhere; no dead tiles).

### FR-B — Per-manager layout
- Layout doc (ordered widget list + size per widget) stored in `manager_preferences` (v2-6's JSONB store — hard dependency), versioned alongside the prefs schema; default layout = today's dashboard.
- Panel: edit mode with add/remove, drag to reorder, size toggle (1×/2×), reset-to-default. Mobile keeps single-column (Omar's phone-first requirement, FR-32) — order still applies.
- Layout follows the account across devices (that is v2-6's job); localStorage only caches.

## 3. Impact map

| Touched | Built in | Change |
|---|---|---|
| Panel DashboardPage | Phase 3/5 (E) | tiles → widget registry + layout renderer + edit mode |
| `manager_preferences` | v2-6 | layout key in the versioned prefs schema |
| `internal/live`/`monitoring` dashboard endpoint | Phase 3 (C) | per-widget data so hidden widgets cost nothing (split or parametrize the aggregate) |

## 4. Acceptance sketch

- Hassan (agent role) opens the picker: sees my-balance and subscribers widgets, does not see revenue-today (no `reports.view`); his phone shows his chosen order single-column.
- Omar reorders and resizes on desktop; his phone reflects it; Sara's dashboard is untouched.
- Reset-to-default restores the stock layout exactly; a widget whose permission is later revoked disappears on next token refresh without a broken tile.

## 5. AI kickoff prompt (paste into a fresh Claude Code session at repo root)

```text
You are working in the HikRAD repo. We are starting v2 phase 10: customizable per-manager dashboards (PRD Decision 32; requires v2-6's manager_preferences to be shipped). You work SOLO — no parallel agents; execute sequentially (widget registry → layout store → edit mode → data-endpoint split), committing in reviewable chunks.

Read, in this order and nothing else yet: CLAUDE.md, docs/v2/phases/00-v2-execution-plan.md, docs/v2/10-custom-dashboards.md, the v2-6 phase brief + gate result (preferences schema/versioning), frontend/panel/src/pages/DashboardPage.tsx, and the dashboard aggregate endpoint it calls.

Step 1 — Amend the docs (single commit): FR rows + Decisions Log row in docs/PRD.md, update the owning sub-PRDs, docs/prd/00-index.md.

Step 2 — Create docs/v2/phases/phase-v2-10-custom-dashboards/00-phase.md with frozen contracts (widget catalog ids + permission map, layout JSON schema + version, endpoint split) and the integration gate (permission-gating test, cross-device layout test, default-equals-today snapshot; migrations only if the prefs schema version bumps — numbers are linear, next free). Scriptable items → scripts/gate-v2-phase-10.sh.

Step 3 — Stop and present the phase brief for my confirmation before writing feature code.

Constraints: permission checks by permission string, never role name; a forbidden widget is absent, not erroring; phone-first single column survives; strings trilingual; update every doc invalidated; record bugs in docs/ops/known-issues.md.
```
