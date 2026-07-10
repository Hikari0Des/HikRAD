# internal/accounting — lossless accounting pipeline (Agent 3)

The engine behind the **hikrad-acct** binary and HikRAD's market wedge: *no
accounting record is ever lost* (success metric M2), and sessions are visible in
the panel within 2 s (M3). Owns FR-31 (backend), FR-33, FR-37, FR-38, FR-40,
NFR-2.

## Shape

```
FreeRADIUS ──POST /acct──▶ ingest ──XADD──▶ acct:stream ──▶ consumer group ──▶ Postgres (sessions, usage_points)
                            │ (Redis down)                     │                └▶ Redis live:sessions (+ index sets)
                            └──fsync WAL──▶ acct-spill.wal ─────┘ (drain on recovery)     └▶ quota:exhausted:<id> (C8)
```

- **Ingest** (`ingest.go`) replies **204 only after a durable enqueue** — on the
  Redis stream, or the fsync'd disk WAL when Redis is down. Anything else is a
  non-2xx so the FreeRADIUS exec script fails closed and the NAS retransmits.
- **Consumer** (`consumer.go`, `sessions.go`) dedups by
  `(nas_id, acct_session_id, record_type, event_time)`, upserts the session
  (32-bit wrap + gigawords + counter-reset math in `record.go`), inserts the
  usage delta keyed by **NAS event time** (out-of-order tolerant), updates the
  live Redis state, evaluates the quota flag, then **XACKs only after the DB
  commit**. DB down → stop acking → the stream grows and ingest spills → drain in
  order on recovery. Zero loss.
- **Reaper** (`reaper.go`) marks silent sessions stale, then closes them with a
  synthesized `reaped` Stop; a late real packet reopens them.
- **Counters** (`counters.go`) are in-process atomics mirrored to
  `pipeline_counters`; `GET /internal/acct/counters` reports every stage plus the
  FR-40 invariant `received − deduplicated − in_queue == persisted`.

The panel-facing side (SSE feed, `live.Count`/`List`, history + usage REST, the
Disconnect action) lives in `internal/live`, sharing the Redis wire format via
`internal/live/livestate`.

## Storage sizing (NFR-3: 200 GB tier)

Mirrors sub-PRD 03's math. At the reference load — **2,000 concurrent sessions,
5-minute interims (12/hr)**:

| Quantity | Value |
|---|---|
| usage_points / hour | 2,000 × 12 = 24,000 |
| usage_points / day | ~576,000 |
| usage_points / year | **~210 M** (pre-compression) |
| bytes / row (uncompressed, incl. overhead) | ~90 B |
| raw / year, uncompressed | ~19 GB |
| after TimescaleDB compression (chunks > 30 days, typ. 10–20×) | ~1–2 GB for the compressed tail |

`sessions` adds one row per session lifecycle (order 100 K–1 M/yr, tens of MB);
`usage_daily` rollups are negligible. Total sits **comfortably under the 200 GB
NFR-3 budget** with years of retention (raw ≥ 12 mo, rollups ≥ 3 yr — the
retention floors enforced in migrations 0131/0133).

## Contract deviations (flagged for the integration gate)

1. **Per-NAS interim interval is not in the `nas` table.** C1-B / this brief say
   to read the interim interval "from B's nas table", but B stores it only as a
   hardcoded `InterimSecs: 300` inside the RouterOS config snippet — there is no
   column. The reaper therefore uses a single service-wide value
   (`HIKRAD_ACCT_INTERIM_SECS`, default 300 s). If per-NAS intervals are wanted,
   B must add a `nas.interim_secs` column and this reaper should read it.
2. **`subscriber_quota_view` (D, C1-D) does not exist yet.** Quota evaluation
   reads the columns C1-D names (`quota_mode`, quota bytes, `cycle_anchor`);
   until D's migrations land the view is absent, so quota evaluation degrades to
   a no-op and re-probes every 5 min. The `quota:exhausted:<id>` key (C8) starts
   being written automatically once the view appears. Exact column names assumed:
   `subscriber_id, quota_mode, quota_total_bytes, quota_down_bytes,
   quota_up_bytes, cycle_anchor` — confirm against D's migration.
3. **Manager scope on live/history needs D's `subscribers.owner_manager_id`.**
   Until that column lands, a *scoped* manager sees no sessions (deny-by-default,
   the safe direction); unscoped admins/operators are unaffected.
4. **Pool utilization (`radius.SetPoolUsageCounter`) is wired to 0.** The live
   state does not carry pool membership, so B's pool-list utilization % shows 0
   this phase. Deferred, not lost.
5. **`deploy/docker/acct.Dockerfile` created outside this agent's paths.** The
   pre-agreed compose block references it; it did not exist, so it was added
   (mirroring `api.Dockerfile`) to make the finalized block buildable.

## Tests

- Pure unit (`record_test.go`, `spill_test.go`, `counters_test.go`): wrap /
  gigaword / reset math, event-time parsing, spill append/drain/corruption,
  counter invariant. Run everywhere.
- DB + Redis-gated chaos (`chaos_test.go`, gated on `HIKRAD_TEST_DB_URL` /
  `HIKRAD_TEST_REDIS_URL`): flood-no-loss + invariant, dedup storm, out-of-order
  interims, spill replay, acct-restart backlog, reaper lifecycle. The full
  kill-Postgres-container orchestration is integration-gate item 3 (compose
  stack); these exercise the same guarantees at code level.
