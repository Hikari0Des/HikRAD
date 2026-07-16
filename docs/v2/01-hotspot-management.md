# v2-01 — Hotspot Management (hotspot-only subscribers + multi-service NAS)

> Deferred to v2 by master PRD Decision 24 (2026-07-11). Reworks structures frozen by Phase 2: the subscriber service model, the FR-58 authorize branch, and the one-type-per-NAS model. Owner sub-PRDs when executed: 04 (subscriber data/rules), 02 (NAS model + auth enforcement), with UI in panel.

## 1. Problem

v1's model is PPPoE-first: every subscriber is a PPPoE account, and Hotspot is a per-subscriber add-on toggle (`allow_hotspot`, FR-58). Two real ISP needs don't fit:

1. **Hotspot-only subscribers.** ISPs sell hotspot-only plans to named customers (dorms, cafés, buildings) who deserve full subscriber records — name, phone, profile, expiry, usage history, portal login — not anonymous vouchers. v1 vouchers cover anonymous walk-ins but carry no subscriber details by design.
2. **Multi-service routers.** A single MikroTik commonly runs **multiple Hotspot server instances** (per interface/SSID/zone) **and** PPPoE server(s) at the same time. v1 models a NAS as having one `type` (PPPoE *or* Hotspot), which forces duplicate NAS entries or misconfiguration.

## 2. Requirements (draft — renumber as FR-61+ at kickoff)

### FR-61 — Subscriber service type
- Replace the `allow_hotspot` boolean with `service_type ∈ {pppoe, hotspot, dual}` (migration maps existing: `allow_hotspot=false → pppoe`, `true → dual`).
- **hotspot-only**: authenticates only on Hotspot services (PPPoE attempts reject `service_not_allowed` — the mirror of today's rule); has full subscriber record, profile, expiry, quota (quota **does** apply to hotspot-only accounts, unlike dual's FR-58.3 exemption which exists to protect the PPPoE quota), portal login, MAC handling per Hotspot semantics.
- **dual** keeps exact FR-58 semantics (+1 hotspot session, hotspot usage quota-exempt, hotspot rate).
- Session limits: hotspot-only uses `session_limit` directly for concurrent hotspot sessions.
- Profiles gain nothing new (hotspot rate fields already exist); pools per service come from FR-62.

### FR-62 — Multi-service NAS
- New `nas_services` child table: per NAS, N rows of (service `pppoe|hotspot`, name/zone label, interface note, ip_pool assignment, hotspot server name for RouterOS matching, enabled).
- Authorize path resolves the *service instance* (from NAS-Port-Type/Called-Station-Id/NAS identification per RouterOS behavior — vendor adapter owns the mapping per FR-17) and applies that service's pool/attributes.
- FR-14 wizard generates config covering **all** enabled services on the router (multiple `/ip hotspot` servers + PPPoE AAA) in one snippet; FR-56 auto-setup preview/apply handles them additively.
- Live sessions, per-NAS graphs, and reports can group by service instance.
- Migration: existing NAS `type` becomes a single seeded `nas_services` row; `type` column retired after backfill.

### FR-63 — Panel/UX
- Subscriber form: service-type selector (radio: PPPoE / Hotspot / Both) replacing the toggle; bulk action updated (`set_allow_hotspot` → `set_service_type`).
- NAS page: services sub-list with per-service status/session counts; wizard steps per service.
- Filters (`service_type`) on user lists, live sessions, reports.

### FR-64 — Subscriber/profile NAS scoping (owner request 2026-07-16, items 2/23/24)
- Subscribers and/or profiles can be **assigned to specific NAS devices and service instances** (nullable = any NAS, the v1 behavior). Assignment UI: NAS + service selectors on the subscriber form, profile form, and the hotspot-account flows.
- **Enforced at RADIUS auth time** (user decision 2026-07-16): an assigned subscriber authenticating through a non-assigned NAS/service is rejected with a new reason `nas_not_allowed` (debug tail + panel localization included).
- Precedence: subscriber assignment overrides profile assignment; both empty = any.
- IP pools follow the service instance (FR-62). **When no HikRAD pool is set anywhere** (subscriber → profile → service), the authorize reply simply omits the `address_pool` intent so the MikroTik falls back to its own locally-configured pool (item 24) — verify this is true today and lock it with a test + document it on the pools screen.
- AuthView/policy cache carries the assignment; `radius.InvalidatePolicy` fires on assignment changes.

## 3. Impact map (why this is v2)

| Touched | Built in | Change |
|---|---|---|
| `subscribers` schema + CRUD + bulk + CSV import mapping | Phase 2/5 (D) | boolean → enum, everywhere it's read |
| `internal/subscribers` AuthView read-model | Phase 2 (D) | expose service_type + per-service pool data |
| `internal/radius` authorize (FR-58 branch, session counting, pool selection) | Phase 2 (B) | full service-matrix rework + service-instance resolution |
| `nas` model, CRUD, wizard snippet, discovery add-flow, FR-56 auto-setup | Phase 2/4 (B) | one-type → services child table |
| Panel subscriber/NAS/live screens | Phase 2/3 (E) | forms, filters, service sub-lists |
| Harness | Phase 1/2 (B) | simulate multi-service NAS + hotspot-only cases |

Nothing here invalidates v1 correctness — v1 ships with the documented PPPoE-first model; this is an expansion, not a fix.

## 4. Acceptance sketch

- Hotspot-only subscriber: PPPoE attempt rejects `service_not_allowed`; hotspot login accepts with hotspot rate; quota enforced; appears in portal with consumed data (Decision 21 rules).
- One router, 2 hotspot servers + 1 PPPoE server: each service authenticates against its own pool; wizard snippet configures all three; live sessions show the service instance.
- Migration: an existing base of mixed `allow_hotspot` subscribers converts losslessly; all Phase-2 policy tests still pass with `pppoe`/`dual` semantics unchanged.

## 5. AI kickoff prompt (paste into a fresh Claude Code session at repo root)

> Execution model (owner decision 2026-07-16, docs/v2/phases/00-v2-execution-plan.md): **one agent, working solo and sequentially** — no parallel agent teams in v2. The role split below (B/D/E) survives only as work-ordering inside the single session.

```text
You are working in the HikRAD repo. v1 is complete and piloted; we are starting v2 phase 1: Hotspot Management + NAS scoping. You work SOLO — do not spawn parallel agents; execute the work areas sequentially in this session (backend model → RADIUS engine → panel UI), committing in reviewable chunks.

Read, in this order and nothing else yet: CLAUDE.md, docs/v2/phases/00-v2-execution-plan.md, docs/v2/01-hotspot-management.md (the brief for this feature), docs/PRD.md §6.1/§6.3 + Decisions 19/21/24, docs/prd/04-subscribers-profiles.md FR-58 section, docs/prd/02-radius-nas-aaa.md.

Step 1 — Amend the docs (single commit): add FR-61/62/63/64 to docs/PRD.md §6 exactly per the brief (renumber if FR numbers moved), a new Decisions Log row, update sub-PRDs 04 and 02 (ownership: FR-61/63 data+UI rules → 04, FR-62/64 + auth-time enforcement → 02), and update docs/prd/00-index.md coverage. Do not contradict Decisions 19 or 21.

Step 2 — Plan the execution: create docs/v2/phases/phase-v2-1-hotspot-management/00-phase.md (frozen contracts: subscriber service_type enum + migration mapping, nas_services schema, subscriber/profile NAS-assignment columns, authorize request/response deltas incl. the new nas_not_allowed reject reason, wizard snippet shape; migration range 0500–0519; integration gate incl. harness-driven multi-service + hotspot-only + nas-scoping auth matrix and a lossless allow_hotspot→service_type migration test). One phase file — no per-agent task files. Scriptable gate items go into scripts/gate-v2-phase-1.sh.

Step 3 — Stop and present the phase brief for my confirmation before writing feature code.

Constraints: vendor neutrality (FR-17) — service-instance resolution from RADIUS attributes lives in internal/radius/vendor/ only; CI greps for violations. All existing Phase-2 policy tests must keep passing for pppoe/dual semantics. Panel strings trilingual (i18n:check is CI-fatal). Update every doc your changes invalidate in the same effort, and record any bug you find+fix in docs/ops/known-issues.md.
```
