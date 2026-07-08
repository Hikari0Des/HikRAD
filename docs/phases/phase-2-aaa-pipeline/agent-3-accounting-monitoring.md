# Phase 2 — Agent 3 (Accounting & Monitoring): lossless pipeline, sessions & usage, live feed, reaper

> Owns FR-31 (backend), FR-33, FR-37, FR-38, FR-40, NFR-2. Depends on contracts in [00-phase.md](00-phase.md) (C1-C, C6, C7-C, C8); parallel with Agents 1, 2, 4, 5.

## Mission & context
This is HikRAD's market wedge: **no accounting record is ever lost** (success metric M2), and operators see sessions live within 2 s (M3). You build `hikrad-acct` (ack-after-durable-enqueue ingest, Redis stream + disk spill, idempotent consumer into TimescaleDB), the sessions/usage data layer with rollups, the stale-session reaper, the audit counters that *prove* zero loss, and the SSE live feed the panel consumes. Detail source: sub-PRD [03-lossless-accounting-live-monitoring](../../prd/03-lossless-accounting-live-monitoring.md) — read §FR-37/38/40 fully.

## File ownership
- **Exclusive:** `backend/cmd/hikrad-acct/**`, `backend/internal/accounting/**`, `backend/internal/live/**`, `backend/migrations/0130_*.sql`–`0139_*.sql`, `deploy/compose.yml` **only** the pre-agreed commented `hikrad-acct` block (uncomment + finalize).
- **Read-only:** harness (B's — you invoke it in tests). **Forbidden:** `internal/{radius,subscribers,billing,auth}` internals, `frontend/**`.

## Tasks
1. Migrations 0130–0139 per phase C1-C: `sessions`, `usage_points` hypertable (+ compression policy > 30 days), `usage_daily` continuous aggregate (per subscriber/NAS/network), `acct_dedup` unique index, `pipeline_counters`. Retention jobs honoring settings floors (raw ≥ 12 mo, rollups ≥ 3 yr). [FR-33, FR-37.4]
2. `hikrad-acct` ingest per C6: HTTP :8082 `/acct`; 204 **only after** XADD to `acct:stream` with Redis AOF everysec, plus a local fsync'd spill WAL when Redis is unavailable, replayed on recovery. Increment `received/enqueued/spilled` counters atomically. [FR-37.1]
3. Consumer group: stream → (a) dedup check via `acct_dedup` insert (conflict → `deduplicated++`, ack, done); (b) upsert `sessions` (Start creates; Interim updates counters/last_interim_at incl. 32-bit wrap + gigawords + counter-reset handling; Stop closes with cause); (c) insert `usage_points` delta; (d) update `live:sessions` Redis hash incl. computed rate (delta ÷ interval); (e) evaluate quota state → write `quota:exhausted:<subscriber_id>` per C8 (read profile quota config via a read-only SQL view D freezes in C1-D — `subscriber_quota_view`); (f) DB-commit then XACK; `persisted++`. DB down → stop acking, stream grows, spill past memory threshold; drain in order on recovery. [FR-37.2/37.3, C8]
4. Stale reaper: no interim for 2× expected interval (per-NAS interim setting from B's nas table, read-only) → `stale=true` (hash + row); after timeout (3× interval + 5 min, settings-overridable) → synthesized Stop, `reaped=true`, counters `reaped++`; late real packets supersede/reopen correctly. [FR-38]
5. Counters + invariant: `GET /internal/acct/counters` → all stages + invariant check bool (`received − deduplicated − in_queue == persisted`); exposed later on health page (Phase 3). [FR-40]
6. Live feed: SSE `GET /api/v1/live/sessions` per C6 (snapshot + upsert/remove, filters, ScopeFilter via A's C2); `live.Count/List` Go interface for B. Session history + usage endpoints per C7-C. [FR-31 backend, FR-33 API]
7. Chaos test suite v1 in `backend/test/` (your files under `internal/accounting` tests + reusable helpers; full `test/chaos/` harness is Phase 5): kill-Postgres-mid-flood, kill-acct-mid-flood, duplicate storms, out-of-order interims — all asserting the invariant. Uses B's harness load mode. [FR-37.5, NFR-2]

Edge cases: Interim arriving before Start (NAS reboot race) → synthesize open session; Stop for unknown session → create closed record + count `orphan_stops`; event_time skew between NAS and server (use NAS `event_time` for usage points, server time for receipt); spill file corruption on unclean shutdown must be detected (checksums) and never crash the drain.

## Contracts consumed/exposed
- **Consumes:** C1-B nas table (read-only: interim interval, id-by-ip), C2 middleware/scoping (A), `subscriber_quota_view` (D, frozen in C1-D), B's FreeRADIUS acct forwarding (C6 shape).
- **Exposes:** C6 live interfaces + SSE (B and E), C7-C usage/session APIs (E now; D's user page + reports later), C8 quota flag (D's AuthView), counters endpoint (Phase-3 health page).

## Definition of done
- Gate items 3 and 4 pass exactly as written (flood + kill + dedup + ≤ 2 s latency + reaper lifecycle).
- Invariant holds after every chaos scenario; counters survive service restart.
- Unit tests: wrap/gigaword/reset math, dedup, reaper state machine, SSE event encoding, scope filtering on live list.
- Sizing note written into the package README: points/day at 2k sessions & 5-min interims vs. NFR-3's 200 GB (mirrors sub-PRD 03 math).

## Handoff
Phase 3 (same role) adds probes/alerts/dashboard on this base; D gets quota flags + usage APIs for the user page; Phase 5 formalizes the chaos suite into the release evidence for M2.
