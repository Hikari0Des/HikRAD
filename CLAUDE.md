# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this project is

HikRAD is a commercial RADIUS AAA + billing platform for Iraqi ISPs (a Snono SAS4 alternative), sold as a one-time license and installed on-premise via Docker. Its differentiator is monitoring: real-time session visibility and a **lossless accounting pipeline** — "never lose an Accounting-Request" is the core product claim (success metric M2) and drives most architectural decisions.

**Current state: documentation only.** There is no source code yet. The repo contains the full planning stack; implementation follows the phased plan below.

## Document hierarchy (order of truth)

1. [docs/PRD.md](docs/PRD.md) — master PRD, the source of truth. All decisions in its Decisions Log are user-confirmed; do not contradict them.
2. [docs/prd/](docs/prd/00-index.md) — 8 domain sub-PRDs elaborating the master (requirement ownership, acceptance criteria, API/data contracts per domain). If a sub-PRD disagrees with the master, the master wins — fix the sub-PRD.
3. [docs/phases/](docs/phases/00-team.md) — multi-agent execution plan: 6 agent roles, 5 phases, one task PRD per agent per phase. Each phase's `00-phase.md` contains **frozen contracts** (API shapes, schema, events) and an integration gate.

Requirement IDs (FR-1…FR-58, NFR-1…NFR-8) are used everywhere; trace any implementation work back to them. Every FR is owned by exactly one sub-PRD (mapping in [docs/prd/00-index.md](docs/prd/00-index.md)). FR-55–58 were added 2026-07-09 (Decisions 16–20: WhatsApp channel + subscriber messaging, NAS auto-discovery/auto-setup, optional Cloudflare tunnel, PPPoE-on-Hotspot dual-service); the affected phase briefs carry dated amendment notes.

## How implementation is meant to proceed

Work is organized so parallel agents don't collide. When implementing:

- Follow the current phase's `docs/phases/phase-N-*/00-phase.md` brief and the relevant `agent-M-*.md` task file. Task files declare exclusive path ownership — respect it.
- Contracts in a phase brief are frozen. If one proves wrong, amend the brief explicitly; never diverge silently.
- Migration files in `backend/migrations/` use numeric ranges assigned per agent per phase (in the phase brief) — never take a number outside your assigned range.
- A phase closes only when its integration gate checklist passes, merged, end to end.

## Planned architecture (fixed by PRD Decision 11)

Go backend · FreeRADIUS 3.2 · PostgreSQL 16 + TimescaleDB · Redis · React 18 + TypeScript · Docker Compose. Single Go module `github.com/hikrad/hikrad` with three binaries:

- `hikrad-api` — REST API (`/api/v1`, chi router) serving panel + portal; also the FreeRADIUS policy endpoint (`rlm_rest` → `POST /internal/radius/authorize`, sub-100 ms p99 budget, Redis-cached read-model).
- `hikrad-acct` — accounting ingest; acks a packet only after durable enqueue (Redis stream + disk spill), consumer upserts sessions/usage into Timescale hypertables. Idempotency key: (nas_id, acct_session_id, record_type, event_time). Audit counters must always satisfy `received − duplicates − in_queue = persisted`.
- `hikrad-monitor` — ICMP/SNMP NAS probes + alerts engine (in-app/Telegram/SMTP).

Key structural rules baked into the plan (Phase 1 contracts):

- Domain packages self-register HTTP modules via an `httpapi` registry — no shared route-file edits.
- Vendor neutrality (FR-17): RADIUS reply attributes are abstract intents; MikroTik VSAs appear only inside the vendor adapter (`internal/radius/vendor/`) — CI greps for violations.
- Money and audit tables are append-only (DB-level REVOKE UPDATE/DELETE); balances are derived from the ledger, never stored-edited.
- Subscriber RADIUS passwords are reversible-encrypted (AES-GCM, key in server config) because CHAP requires cleartext at auth time — decryption happens only in the authorize path (NFR-4.2).
- Frontends: `frontend/panel` (admin) and `frontend/portal` (subscriber) consume `frontend/shared` (`@hikrad/shared`) for i18n. Trilingual (en/ar/ku), true RTL via CSS logical properties, charts and usernames/MACs/IPs stay LTR inside RTL. **No hardcoded user-visible strings** — `npm run i18n:check` is CI-fatal.
- Nothing required for daily operation may depend on internet access (NFR-7): license validation is offline, e-wallet payments are the only online-dependent feature and must degrade gracefully.

## Commands (planned, per Phase 1 — verify once scaffolded)

The Phase 1 plan defines `make up / down / seed / test / migrate / lint` at the repo root, `npm run i18n:check` under `frontend/`, and a RADIUS packet-test harness (`make test-harness-smoke`, source in `backend/test/harness/`) that simulates a MikroTik NAS. Until Phase 1 lands, none of these exist.

## Domain context worth knowing

- Personas gauge every UX decision: Sara (front desk, low technical, ≤ 3 clicks), Omar (owner, dashboard-on-phone), Ali (network engineer, MikroTik expert), Hassan (field agent, phone-first, balance-driven), Noor (subscriber, portal).
- Timezone Asia/Baghdad, currency IQD; Arabic text handling (including CP1256 in CSV imports) is a first-class requirement, not an edge case.
- "Expired" subscribers are usually not cut off — they're moved to a walled-garden IP pool with a renewal redirect, and renewal restores full speed via CoA without re-dialing (key flow 2). This renew→CoA path is the product's hero flow.
