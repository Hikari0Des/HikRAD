# HikRAD — Multi-Agent Execution Plan: Team & Phases

> Generated 2026-07-08 from [docs/PRD.md](../PRD.md) v1.0 and sub-PRDs [docs/prd/](../prd/00-index.md). Plan confirmed by the product owner on 2026-07-08.

## Project snapshot

HikRAD: on-premise RADIUS AAA + billing platform for Iraqi ISPs (SAS4 alternative) — FreeRADIUS 3.2 + Go services (`hikrad-api`, `hikrad-acct`, `hikrad-monitor`), PostgreSQL 16 + TimescaleDB, Redis, React 18 + TS panel & portal (PWA), Caddy, Docker Compose, trilingual RTL (ar/ku/en). Specialty and market wedge: live monitoring + a lossless accounting pipeline. v1 = pilot ISP running all subscribers for 30 days with zero lost accounting records.

## Team Composition & Agent Roles

| Agent | Role | Mission | Expertise | Global path ownership | Must never touch |
|---|---|---|---|---|---|
| **A** | Platform & Security | Deployable product shell (Compose, installer, CI, license, backup, settings) and identity/authz (managers, roles, 2FA, audit) | Docker, Linux ops, Go, appsec/OWASP, crypto-at-rest | `deploy/` (exc. `deploy/freeradius/`), `scripts/`, `.github/`, `backend/internal/platform/`, `backend/internal/auth/`, `docs/ops/` | RADIUS packet logic, business/billing rules, frontend code |
| **B** | RADIUS & NAS | Everything a MikroTik talks to: FreeRADIUS wiring, authorize policy endpoint, NAS mgmt, CoA, IP pools, vendor adapters, packet harness | FreeRADIUS, RADIUS/CoA protocol, RouterOS, Go | `deploy/freeradius/`, `backend/internal/radius/`, `backend/test/harness/` | Ledger/billing code, pipeline internals, frontend code |
| **C** | Accounting & Monitoring | Lossless accounting pipeline, sessions/usage time-series, live data feeds, health probes, alerts engine, chaos/perf proof | Go concurrency, Redis streams, TimescaleDB, SNMP/ICMP, load testing | `backend/cmd/hikrad-acct/`, `backend/cmd/hikrad-monitor/`, `backend/internal/accounting/`, `backend/internal/live/`, `backend/internal/monitorsvc/`, `backend/test/chaos/` | Auth policy decisions, money math, frontend code |
| **D** | Backend Business | The `/api/v1` framework and all business domains: subscribers, profiles, billing/ledger/vouchers, payment gateways, portal API, reports API | Go, REST design, PostgreSQL, payment integrations | `backend/cmd/hikrad-api/`, `backend/internal/httpapi/`, `backend/internal/subscribers/`, `backend/internal/profiles/`, `backend/internal/billing/`, `backend/internal/portalapi/`, `backend/internal/reports/` | FreeRADIUS config, pipeline consumers, auth middleware internals, frontend code |
| **E** | Frontend Panel | The entire admin/manager React app: shell, users, NAS, live sessions, dashboard, billing UIs, reports UIs | React+TS, data-dense UIs, WebSocket/SSE, RTL layouts | `frontend/panel/` | Portal app, `frontend/shared/` (consumes only), any backend code |
| **F** | Frontend Portal & Localization | Subscriber portal, the shared i18n/RTL framework both apps use, PWA packaging of both apps | React+TS, i18n/bidi, PWA/service workers/Web Push, mobile-first UX | `frontend/portal/`, `frontend/shared/` (+ in Phase 4 only: `frontend/panel/public/` PWA assets — E is not staffed that phase) | Panel screens/components (Phase 4 PWA-asset exception aside), any backend code |

Migrations live in `backend/migrations/`; each phase brief assigns each agent an exclusive numeric range (e.g. `0100–0109`), so no two agents ever create the same file.

## Phase outline

```
Phase 1: Foundation & Contracts — repo scaffold, running stack, frozen conventions, first static Access-Accept
  Agent 1 (Platform & Security): Compose stack + env/secrets + migration tooling + CI + settings skeleton (FR-49 base, FR-53)
  Agent 2 (RADIUS & NAS): FreeRADIUS container wired via rlm_rest to a stub authorize endpoint + MikroTik packet harness (FR-17 base, NFR-8)
  Agent 3 (Backend Business): Go monorepo + /api/v1 framework (errors, pagination, module registry) + core schema + seed (FR-52)
  Agent 4 (Frontend Panel): panel scaffold — RTL-capable shell, routing, login screen against stub auth
  Agent 5 (Frontend Portal & Localization): frontend/shared i18n/RTL framework + locale pipeline + portal skeleton (NFR-6 foundation)
Phase 2: AAA Core & Lossless Pipeline — a real subscriber authenticates, accounting is lossless, sessions are live on screen
  Agent 1 (Platform & Security): manager login, tokens, permission middleware, scoping, audit-write API (FR-28/29 core)
  Agent 2 (RADIUS & NAS): NAS CRUD + RouterOS wizard snippets, CoA service, IP pools, vendor adapter, full authorize policy (FR-13–17)
  Agent 3 (Accounting & Monitoring): lossless pipeline + sessions/usage hypertables + reaper + audit counters + live WS (FR-31/33/37/38/40, NFR-2)
  Agent 4 (Backend Business): subscribers + profiles CRUD, search, bulk, auth read-model + policy rules (FR-1–5, 7–10)
  Agent 5 (Frontend Panel): NAS screens, user list/detail v1, global search, Live Sessions table (FR-2/3/31 UI)
Phase 3: Billing, Security & Monitoring — the MVP gate: renew-with-CoA, money ledger, roles/2FA, dashboard, alerts
  Agent 1 (Platform & Security): roles/permissions complete, TOTP 2FA, panel session mgmt, IP allowlist, audit viewer API (FR-27–30)
  Agent 2 (RADIUS & NAS): expiry/quota CoA enforcement worker, Hotspot login template, RADIUS debug decision stream (FR-9/10 runtime, FR-18, FR-39)
  Agent 3 (Accounting & Monitoring): ICMP/SNMP health probes, alerts engine + channels, dashboard API, system self-monitoring (FR-32, 34–36)
  Agent 4 (Backend Business): renewals, ledger, agent balances, receipts, vouchers, refunds, burst/time-of-day (FR-19–22, 24–25, FR-11)
  Agent 5 (Frontend Panel): dashboard, renew dialog, ledger/voucher/balance UIs, managers/roles/2FA UI, alerts UI
Phase 4: Portal, Payments & PWA — Noor renews at midnight from her phone
  Agent 1 (RADIUS & NAS): ROS 6/7 quirk matrix + CoA hardening + walled-garden entries for payment/portal hosts
  Agent 2 (Accounting & Monitoring): Web Push channel + expiring-soon digest + usage API polish (FR-36.3, FR-54.4 backend)
  Agent 3 (Backend Business): portal API + PaymentGateway interface + mock adapter + first live adapter(s) (FR-23, FR-41/42 backend)
  Agent 4 (Frontend Portal & Localization): portal UI in 3 languages, voucher/e-wallet flows, PWA packaging of both apps (FR-41–44, FR-54, NFR-6)
Phase 5: Reports, Install & License — v1: installable in <30 min, provable zero loss, pilot-ready
  Agent 1 (Platform & Security): license system, backup/restore, update mechanism, install.sh + first-run wizard backend, ops docs (FR-50/51, FR-49)
  Agent 2 (Accounting & Monitoring): chaos + performance verification suites, counter-invariant proof, NFR-1/2 evidence (M2)
  Agent 3 (Backend Business): reports APIs, CSV import wizard backend, scheduled digests (FR-45–47, FR-6, FR-48)
  Agent 4 (Frontend Panel): reports/settings/import/first-run-wizard UIs, ≤3-click + empty-state polish (NFR-5)
```

## How to run it

For each phase, in order:
1. Read `phase-N-<slug>/00-phase.md` (goal, **frozen contracts**, path map, integration gate).
2. Spawn one coding agent per `agent-M-*.md` file in that folder, **in parallel**; each task file is the agent's complete standalone briefing.
3. When all agents finish, merge and run the **integration gate** checklist in the phase brief. The gate must pass before Phase N+1 starts — later phases assume earlier gates are green.
4. Contracts in a phase brief are frozen for that phase; if one proves wrong, stop the affected agents, amend the brief, restart the tasks — never renegotiate silently mid-flight.

## Audit results

- **Coverage:** all 40 Must FRs and all 9 Should FRs (FR-6, 7, 11, 18, 25, 30, 39, 47, 53) are assigned to ≥ 1 task; every assignment cites its FR IDs. Backend/frontend pairs are deliberately dual-assigned (e.g. FR-31 backend→C / UI→E; FR-22 backend→D / UI→E / portal→F) — listed in each phase brief. All 8 NFRs land in named tasks (NFR-1 → B/C + gate benchmarks; NFR-2 → C; NFR-3/7 → A; NFR-4 → A; NFR-5 → E; NFR-6 → F; NFR-8 → A/B/C). **Deliberately deferred (Could):** FR-12 (profile-change scheduling), FR-26 (promo pricing), FR-44 (portal password self-change), FR-48 beyond the daily digest (arbitrary scheduled reports) — stretch items only if a phase finishes early; noted in their owning sub-PRDs.
- **Collision:** within every phase, agent path ownership is pairwise disjoint (verified per phase brief's path map; migration files are range-partitioned per agent; the single cross-boundary case — PWA assets in `frontend/panel/public/` during Phase 4 — is safe because Agent E is not staffed in Phase 4, and is recorded in both role table and phase brief).
- **Dependency:** no task consumes a contemporaneous task's *unfrozen* output. Everything crossing agent lines inside a phase (authorize contract, auth-view read-model, CoA interface, `InvalidatePolicy`, accounting ingest shape, enforcement events, portal/payment/report API shapes) is written into the phase brief as a frozen contract with exact request/response shapes; sequencing needs that couldn't be frozen were moved across phase boundaries (e.g. real manager auth is Phase 2 so Phase 1's panel uses a seeded stub; live gateway adapters are Phase 4 after the ledger exists).
