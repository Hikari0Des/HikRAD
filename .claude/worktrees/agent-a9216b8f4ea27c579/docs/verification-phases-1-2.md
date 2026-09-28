# Verification — Phases 1 & 2: docs vs. code (2026-07-11)

**Verdict: the completed phases are accurately documented. No redo of Phase 1 or Phase 2 is required, and none of the v1 additions decided on 2026-07-10/11 (FR-59 scratch cards, FR-60 device monitoring, Decision-21 portal rules) touch Phase-1/2 code.** The only items that *would* rework Phase-2 code (hotspot-only subscribers, multi-service NAS) were deferred to v2 for exactly that reason (Decision 24, briefs in [docs/v2/](v2/00-v2-index.md)).

Method: structural spot-check of every load-bearing claim in `CLAUDE.md`, the Phase-1/2 briefs, and the sub-PRDs against the working tree, on top of the recorded gate results (Phase-1 gate green 2026-07-09; Phase-2 agents' definitions of done met with full backend suite + 28 panel tests green).

## Checks performed

| Documented claim | Where documented | Code reality | Match |
|---|---|---|---|
| Two binaries so far: `hikrad-api`, `hikrad-acct` (`hikrad-monitor` = Phase 3) | CLAUDE.md, PRD §8 | `backend/cmd/{hikrad-api,hikrad-acct}` exist, no monitor | ✅ |
| Module registry: domain packages self-register; `modules.go` = blank imports only (auth, platform, profiles, live, radius, subscribers; billing/portalapi/reports commented for later phases) | CLAUDE.md, Phase-1 C3 | [modules.go](../backend/cmd/hikrad-api/modules.go) exactly as described | ✅ |
| Internal packages per path-ownership map | CLAUDE.md table | `internal/{accounting,auth,httpapi,live,platform,profiles,radius,seed,subscribers}` — no strays | ✅ |
| Migration ranges: Phase-1 base + Phase-2 A `0110–0112`, B `0120–0122`, C `0130–0135`, D `0100–0103`; no collisions | Phase-2 brief | 20 files: `0001,0002,0010,0011` (P1) + `0100–0103, 0110–0112, 0120–0122, 0130–0135` — all in-range, disjoint | ✅ |
| Radius package owns authorize (PAP+CHAP), AuthView consumption, NAS CRUD, pools, CoA, MNDP discovery, RouterOS snippet, vendor adapter isolation | CLAUDE.md, sub-PRD 02 | `internal/radius/`: `authorize.go, chap.go, authview.go, policy.go, engine.go, nas_api.go, pools_api.go, coa.go, discover.go, snippet.go, vendor/` + tests incl. `coa_roundtrip_test.go` | ✅ |
| Lossless pipeline: ack-after-durable-enqueue, spill, consumer/dedup, reaper, counters, quota tracking; chaos tests exist | CLAUDE.md, sub-PRD 03 FR-37/38/40 | `internal/accounting/`: `ingest.go, queue.go, spill.go, consumer.go, record.go, reaper.go, counters.go, quota.go, chaos_test.go` + tests | ✅ |
| Real auth: login/refresh, argon2id, lockout/rate-limit, permissions by string, audit append-only, panel sessions | Phase-2 agent-1 status | `internal/auth/`: `login.go, tokens.go, password.go, ratelimit.go, permissions.go, middleware.go, audit.go, auditlog_api.go, panelsessions_api.go` + tests incl. redaction | ✅ |
| FR-58 dual-service enforced (allow_hotspot flag, hotspot rate, `service_not_allowed` reject) | PRD Decision 19 | `subscribers/api.go` (field + persistence), `authorize_e2e_test.go` covers accept/reject/rate branches, bulk action `set_allow_hotspot` | ✅ |
| Panel screens: login, subscribers, profiles, NAS, pools, live sessions; shared i18n locales en/ar/ku | Phase-2 agent-5 status | `frontend/panel/src/pages/{subscribers,profiles,nas,pools,live}` + `LoginPage`, `DashboardPage` (placeholder for P3), `RtlSmokePage`; `frontend/shared/locales/*/{common,panel,portal}.json` | ✅ |
| Harness modes: smoke (PAP/CHAP), rate/duration load, `mndp-announce`, `coa-listen` | CLAUDE.md commands | `test/harness/main.go` flags + README document all four | ✅ |
| Compose stack: deploy/{compose.yml, caddy, freeradius, docker} | Phase-1 brief | present | ✅ |

## Minor doc nits found (no action required now)

1. **`backend/test/chaos/` doesn't exist yet** — 00-team.md lists it under Agent C's global paths, but Phase-2 chaos coverage was implemented as `internal/accounting/chaos_test.go`. Correct behavior: Phase 5 (agent-2, evidence pack) creates `backend/test/chaos/**` per its brief; nothing to fix today.
2. `DashboardPage.tsx` exists as a shell placeholder ahead of its Phase-3 requirement — consistent with the Phase-2 handoff notes ("slots" left for Phase 3), just noting it predates its FR.
3. Known flagged seams from the Phase-2 agent statuses (unfiltered list endpoint → panel filters client-side; per-NAS interim column absent) are recorded in the agents' handoffs and are Phase-3-addressable — they are documented debt, not doc/code drift.

## Consequence for planning

- **Phases 1–2: closed, accurate, no rework.** v1 continues with Phase 3 as planned.
- **Anything that would have forced Phase-1/2 rework is in v2** with briefs + kickoff prompts: [v2-01 Hotspot management](v2/01-hotspot-management.md) (subscriber `service_type`, multi hotspot/PPPoE servers per router), v2-02 AsiaHawala/Areeba adapters (deferred for merchant-account reasons, not code reasons; *withdrawn 2026-07-17 and replaced by [manual payment providers](v2/02-manual-payment-providers.md) — PRD Decisions 29/30*).
