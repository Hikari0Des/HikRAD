# v2-03 — NAS auto-setup config manager (form-driven values + read/modify existing config)

> Owner request 2026-07-16 (item 10). v1's FR-56 auto-setup applies a fixed, additive-only plan computed from hard defaults; conflicts abort the whole apply. The owner wants to *see* what the router currently has, *choose* the values, and *modify* existing entries — not only add missing ones.

## 1. Problem

1. **Defaults, not choices.** v1's preview derives every value (RADIUS server address, interim interval, walled-garden hosts, hotspot profile names) from settings/constants. Operators (persona Ali) want a form: edit each value before previewing.
2. **Additive-only.** v1 refuses to touch a router that already has a conflicting `/radius` entry or AAA config. Real onboarding often means *taking over* an existing config: show what's there, and let the operator choose per item — keep / update to HikRAD's value / add alongside.
3. **No config visibility.** There is no read-only "current RADIUS-relevant config" view per NAS at all.

## 2. Requirements (draft — renumber as FR-6x at kickoff)

### FR-A — Config inspection (read-only)
- `GET /api/v1/nas/{id}/config` — connects with the saved API credentials and returns the router's current RADIUS-relevant state: `/radius` entries, `/radius incoming`, `/ppp aaa`, `/ip hotspot profile` AAA fields, walled-garden entries, plus `/system resource` version (reuses the v1.1 probe). Pure print sentences; audit-logged.
- Panel: "Current config" tab in the NAS auto-setup modal rendering this as a readable diff-source.

### FR-B — Form-driven plan input
- The auto-setup modal becomes a two-step wizard: **(1) values form** (RADIUS server IP, secret handling, CoA port, interim interval, walled-garden list, per-service toggles — pre-filled from HikRAD settings and, where readable, from the router's current values), **(2) preview** (computed from the form, not from constants).
- The form's values feed `PlanAutoSetup` via an extended `SnippetInput`; the copy-paste snippet endpoint accepts the same overrides so both paths stay consistent.

### FR-C — Modify-or-create per item
- Preview items gain a third state besides add/ok: **conflict with resolution choice** — for each conflicting entry the operator picks `keep existing` / `update to planned value` / `abort`. Default remains abort-on-conflict (Decision 17's safety stance); updates are explicit opt-ins recorded in the audit log with before/after.
- Apply executes adds + chosen updates in order, still hash-gated: the hash now covers the chosen resolutions, so a router-state change between preview and apply still aborts.
- Never delete anything. Deletion stays manual, by design.

## 3. Impact map

| Touched | Built in | Change |
|---|---|---|
| `internal/radius/vendor` PlanAutoSetup/ApplyAutoSetup | Phase 4 (B) | read current values into the plan; per-item update sentences; extended SnippetInput |
| `internal/radius` autosetup_api + snippet | Phase 4 (B) | new /config endpoint; plan-input body; resolution choices in preview/apply contract |
| Panel NasAutoSetupModal | Phase 4 (E) | two-step wizard + current-config tab + per-conflict choice UI |
| docs/ops/ros-matrix.md | Phase 4 | new "update-in-place" legs need pilot validation before apply is enabled per version |

## 4. Acceptance sketch

- A router with a foreign `/radius` entry: preview shows it with keep/update options; choosing update rewrites it to HikRAD's values on apply and the audit log holds before/after; choosing keep leaves it and adds nothing conflicting.
- The values form pre-fills from the router (interim interval, walled garden) when readable; changed values appear verbatim in both the preview sentences and the copy-paste snippet.
- Nothing is ever deleted; abort-on-conflict remains the default; a stale preview (router changed) still refuses to apply.

## 5. AI kickoff prompt (paste into a fresh Claude Code session at repo root)

```text
You are working in the HikRAD repo. v1 is complete; we are starting v2 phase 2: NAS auto-setup config manager. You work SOLO — no parallel agents; execute sequentially (vendor layer → API → panel), committing in reviewable chunks.

Read, in this order and nothing else yet: CLAUDE.md, docs/v2/phases/00-v2-execution-plan.md, docs/v2/03-nas-autosetup-config-manager.md, docs/prd/02-radius-nas-aaa.md FR-56, backend/internal/radius/autosetup_api.go, backend/internal/radius/vendor/mikrotik_autosetup.go.

Step 1 — Amend the docs (single commit): new FR rows in docs/PRD.md §6 + Decisions Log row (per-item modify-or-create supersedes the abort-only half of Decision 17 — additively, abort stays the default), update sub-PRD 02, docs/prd/00-index.md.

Step 2 — Create docs/v2/phases/phase-v2-2-autosetup-config-manager/00-phase.md with frozen contracts (GET /nas/{id}/config response shape, plan-input request body, preview item states + resolution enum, hash coverage) and the integration gate (fake-router unit matrix + a live-router checklist item; migration range 0520–0529 if any). Scriptable gate items go into scripts/gate-v2-phase-2.sh.

Step 3 — Stop and present the phase brief for my confirmation before writing feature code.

Constraints: all RouterOS sentences stay inside internal/radius/vendor/ (FR-17; CI greps). Never emit a delete sentence. Panel strings trilingual. Update every doc your changes invalidate; record bugs found in docs/ops/known-issues.md.
```
