# v2-03 — NAS auto-setup config manager (form-driven values + read/modify existing config + server management)

> Owner request 2026-07-16 (item 10), **expanded 2026-07-17 (PRD Decision 31)**: not just RADIUS wiring — manage the Hotspot and PPPoE **servers themselves**, both the router's existing ones and new ones HikRAD creates. v1's FR-56 auto-setup applies a fixed, additive-only plan computed from hard defaults; conflicts abort the whole apply. The owner wants to *see* what the router currently has, *choose* the values, *modify* existing entries — and *stand up new servers* from a panel form.

## 1. Problem

1. **Defaults, not choices.** v1's preview derives every value (RADIUS server address, interim interval, walled-garden hosts, hotspot profile names) from settings/constants. Operators (persona Ali) want a form: edit each value before previewing.
2. **Additive-only.** v1 refuses to touch a router that already has a conflicting `/radius` entry or AAA config. Real onboarding often means *taking over* an existing config: show what's there, and let the operator choose per item — keep / update to HikRAD's value / add alongside.
3. **No config visibility.** There is no read-only "current RADIUS-relevant config" view per NAS at all.
4. **Servers are managed only from Winbox.** HikRAD knows a NAS's service instances (`nas_services`, FR-62) but can only *discover* them — creating a new hotspot zone or PPPoE service, or changing one's pool/profile/interface, still means leaving HikRAD for the router UI (owner request 2026-07-17).

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

### FR-D — Hotspot & PPPoE server management (PRD Decision 31)
- Every `nas_services` instance carries a **management mode**: `router` (pre-existing on the router; HikRAD discovered/adopted it — read-only from HikRAD until explicitly adopted for editing) or `system` (HikRAD created it and owns its config).
- **List**: the NAS services screen shows each instance's real router-side config (hotspot: interface, address pool, hotspot+user profiles, RADIUS flags; PPPoE: service-name, interfaces, default profile, authentication) read live over the API — reusing FR-A's inspection plumbing.
- **Create (system-managed)**: panel forms to stand up a new Hotspot server (interface, local address/pool, profile values, walled-garden per FR-56, RADIUS wired to HikRAD, incl. the FR-62.7 `address-pool=none` user-profile guard) or a new PPPoE server (interface(s), service-name, profile with RADIUS auth) — planned/previewed/applied through the same hash-gated pipeline as FR-B/C, then registered in `nas_services` automatically.
- **Edit**: system-managed instances can be re-formed and re-applied (diff preview, before/after audit). Router-managed instances offer **adopt** (HikRAD reads the full current config into a form, the operator confirms, mode flips to `system`) — adoption is explicit, never silent.
- **Never delete** a server from HikRAD; disabling an instance in HikRAD only stops HikRAD from serving it (FR-62 semantics unchanged).

## 3. Impact map

| Touched | Built in | Change |
|---|---|---|
| `internal/radius/vendor` PlanAutoSetup/ApplyAutoSetup | Phase 4 (B) | read current values into the plan; per-item update sentences; extended SnippetInput; server create/edit/adopt plans |
| `internal/radius` autosetup_api + snippet + nas_services | Phase 4 / v2-1 (B) | new /config endpoint; plan-input body; resolution choices in preview/apply contract; management-mode column + server CRUD endpoints |
| Panel NasAutoSetupModal + NAS services editor | Phase 4 / v2-1 (E) | two-step wizard + current-config tab + per-conflict choice UI; server create/edit/adopt forms |
| docs/ops/ros-matrix.md | Phase 4 | new "update-in-place" + server-provisioning legs need pilot validation before apply is enabled per version |

## 4. Acceptance sketch

- A router with a foreign `/radius` entry: preview shows it with keep/update options; choosing update rewrites it to HikRAD's values on apply and the audit log holds before/after; choosing keep leaves it and adds nothing conflicting.
- The values form pre-fills from the router (interim interval, walled garden) when readable; changed values appear verbatim in both the preview sentences and the copy-paste snippet.
- From the panel alone, Ali creates a second hotspot zone on an existing NAS (new server + profile + pool + RADIUS wiring, `address-pool=none` guard included) and a subscriber logs in through it; the instance appears in `nas_services` as system-managed with live config shown.
- A pre-existing PPPoE server shows its real router config read-only; adopting it flips it to system-managed and lets the operator change its default profile from HikRAD, with before/after in the audit log.
- Nothing is ever deleted; abort-on-conflict remains the default; a stale preview (router changed) still refuses to apply.

## 5. AI kickoff prompt (paste into a fresh Claude Code session at repo root)

```text
You are working in the HikRAD repo. v1 is complete; we are starting v2 phase 3: NAS auto-setup config manager + hotspot/PPPoE server management (PRD Decision 31). You work SOLO — no parallel agents; execute sequentially (vendor layer → API → panel; config manager before server management, which builds on it), committing in reviewable chunks.

Read, in this order and nothing else yet: CLAUDE.md, docs/v2/phases/00-v2-execution-plan.md, docs/v2/03-nas-autosetup-config-manager.md, docs/prd/02-radius-nas-aaa.md FR-56 + FR-62, backend/internal/radius/autosetup_api.go, backend/internal/radius/vendor/mikrotik_autosetup.go, and the v2-1 phase brief's nas_services contract.

Step 1 — Amend the docs (single commit): new FR rows in docs/PRD.md §6 + Decisions Log row (per-item modify-or-create supersedes the abort-only half of Decision 17 — additively, abort stays the default), update sub-PRD 02, docs/prd/00-index.md.

Step 2 — Create docs/v2/phases/phase-v2-3-autosetup-config-manager/00-phase.md with frozen contracts (GET /nas/{id}/config response shape, plan-input request body, preview item states + resolution enum, hash coverage, nas_services management-mode column + server create/edit/adopt endpoint shapes) and the integration gate (fake-router unit matrix + a live-router checklist item incl. creating a real hotspot zone end-to-end; migration budget 0520–0529 — numbers are linear, take the next free ones). Scriptable gate items go into scripts/gate-v2-phase-3.sh.

Step 3 — Stop and present the phase brief for my confirmation before writing feature code.

Constraints: all RouterOS sentences stay inside internal/radius/vendor/ (FR-17; CI greps). Never emit a delete sentence — server removal stays manual on the router. Adoption of a router-managed server is explicit, never silent. New hotspot servers always include the FR-62.7 address-pool=none user-profile guard. Panel strings trilingual. Update every doc your changes invalidate; record bugs found in docs/ops/known-issues.md.
```
