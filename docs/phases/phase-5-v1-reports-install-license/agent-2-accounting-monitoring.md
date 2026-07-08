# Phase 5 — Agent 2 (Accounting & Monitoring): chaos & performance evidence pack

> Owns NFR-1/NFR-2 verification, M2 proof (FR-37.5, FR-40 as evidence). Depends on contracts in [00-phase.md](00-phase.md) (C6); parallel with Agents 1, 3, 4.

## Mission & context
HikRAD's sales claim is "zero lost accounting records" (success metric M2) and the master PRD backs it with named mitigations: chaos tests and audit counters. This phase converts your Phase-2/3 test helpers into a formal, scripted, reproducible **evidence pack** run against the release candidate at full target scale (5k subscribers / 2k concurrent sessions / 50 pkt/s bursts), plus fixes for whatever the runs uncover. This is verification work: the features exist; the proof doesn't yet. Detail source: sub-PRD [03-lossless-accounting-live-monitoring](../../prd/03-lossless-accounting-live-monitoring.md) §FR-37.5, NFR-2; master NFR-1.

## File ownership
- **Exclusive:** `backend/test/chaos/**`, `backend/test/perf/**`, `backend/internal/accounting/**`, `backend/internal/monitorsvc/**`, `docs/evidence/**`.
- **Read-only:** B's harness (invoke, don't modify — file needs to B's… B is unstaffed this phase, so harness gaps are yours to raise at merge; minor flag additions allowed **only** in `backend/test/chaos|perf` wrappers). **Forbidden:** `internal/{billing,reports,auth,platform,radius}`, `frontend/**`.

## Tasks
1. **Chaos suite** (`test/chaos/`, FR-37.5/NFR-2): scripted scenarios against a compose stack seeded at 5k/2k scale — kill Postgres 10 min mid-50 pkt/s flood; kill hikrad-acct mid-flood; kill Redis (spill WAL path); unclean host reboot (VM reset); NAS retransmit storm (3× dup); out-of-order/backdated interims; panel-down-auth-up (NFR-2). Each asserts: counter invariant (received − dups − queued = persisted), session-state consistency, no data corruption post-recovery.
2. **Perf suite** (`test/perf/`, NFR-1): auth p99 < 100 ms at 50 req/s burst over 2k live sessions; sustained 7 pkt/s + 50 pkt/s burst ingest with queue depth ≈ 0 steady-state; SSE packet-to-screen ≤ 2 s (scripted headless client); panel API p95 < 1.5 s page-budget checks on the heavy endpoints (dashboard, user list, usage graphs); resource ceilings within NFR-3 (4 vCPU/8 GB — measure headroom).
3. **Sizing verification** (NFR-3): 12-month data simulation (generator writing compressed hypertable chunks) → confirm 200 GB fits raw ≥ 12 mo + rollups ≥ 3 yr with measured compression ratios; retention jobs verified against settings floors.
4. **Evidence pack** (`docs/evidence/`, C6): one `make evidence` entrypoint producing a dated report (environment, versions, scenario results, perf numbers, sizing table, pass/fail vs. targets) — the artifact shipped with the v1 release and re-runnable at the pilot.
5. **Fix what the runs find** in your paths (pipeline/monitor); anything outside → merge-time findings list with reproduction scripts.

Edge cases the suite must specifically hunt: spill-file replay after partial write (checksum path); counter persistence across restart mid-drain; reaper vs. recovery race (NAS returns while reap timer pending); Redis AOF everysec 1 s window under hard power loss (document measured behavior and the mitigation stance per sub-PRD 03's open question — resolve it with data).

## Contracts consumed/exposed
- **Consumes:** B's harness load/scenario modes, seeded fixtures (D), the full running stack.
- **Exposes:** `make evidence` + `docs/evidence/` (release artifact; A's pilot checklist references it); findings list for merge.

## Definition of done
- Gate item 6 passes: all chaos scenarios green with invariant proof; NFR-1 numbers met and recorded; sizing confirmed.
- Suite runs in CI-nightly mode (reduced scale) and full mode via `make evidence` (documented runtime).
- The Redis-durability open question closed with measured data and a recorded decision.

## Handoff
v1 ships with proof. The pilot (M1/M2) re-runs `make evidence` on real hardware; nightly CI guards regressions post-v1.
