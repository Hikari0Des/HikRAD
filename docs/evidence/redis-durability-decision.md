# Redis AOF durability — decision record

> Closes sub-PRD 03 ([03-lossless-accounting-live-monitoring.md](../prd/03-lossless-accounting-live-monitoring.md)) §7's open question: *"Redis AOF `appendfsync everysec` admits a ≤ 1 s durability window on hard power loss; decide whether the ack path needs the disk WAL in front of Redis by default, or only as failover — measure both against NFR-1 in P2."* Phase 5, Agent 2 (Accounting & Monitoring), 2026-07-14.

## The question

`deploy/compose.yml`'s Redis runs `--appendonly yes --appendfsync everysec`: durable within ~1 second, not per-write. `hikrad-acct`'s ack contract (FR-37.1) is "204 only after a durable enqueue" — the enqueue is a Redis `XADD`. If Redis is hard-killed (SIGKILL / host power loss) within that ~1s window after an `XADD` returned success, the write can be lost even though the NAS was told (via the 204) that it was durable — the NAS never retransmits a record it believes succeeded.

## Method

`backend/test/chaos`'s `redis-durability` scenario: burst 30 sessions' worth of Start/Stop records, hard-kill (`docker kill`, not a graceful stop) the Redis container within 200ms of the last ack, restart it, and measure `sent - persisted_delta` after full recovery. Repeated as part of the `unclean-reboot` scenario too (which hard-kills Postgres + Redis + `hikrad-acct` simultaneously — a more realistic compound failure than killing Redis alone).

## Measured data

| Configuration | Scenario | Trials | Records/trial | Records lost |
|---|---|---|---|---|
| `appendfsync everysec` (current shipped default) | `redis-durability` (Redis-only hard-kill) | 1 | 60 | 0 |
| `appendfsync everysec` (current shipped default) | `unclean-reboot` (Postgres+Redis+acct hard-killed together) | 3 | 120 | 0, 1, 1 |
| `appendfsync always` (fsync every write) | `unclean-reboot` (Postgres+Redis+acct hard-killed together) | 3 | 80 each | 0, 0, 0 |

`everysec` did **not** lose data on every trial — the window is real but narrow (records land in the AOF buffer and get fsynced within the same second more often than not on a lightly-loaded box) — but it is not zero-risk: 2 of 4 trials across both scenarios lost exactly 1 record, consistent with a genuine sub-second durability gap, not test-harness noise (the lost record was never reflected in `sessions`/`usage_points` either — confirmed via direct row counts, not just the counter). `appendfsync always` showed zero loss across every trial run, as expected (each `XADD` is fsynced before the client's `XADD` call returns, so a kill immediately after can never lose an already-acked write).

## Decision

**Switch the default to `appendfsync always`.** The measured perf cost (see `backend/test/perf/ingest`) is well inside the NFR-1 ingest budget at the 5k/2k reference load — 7–50 pkt/s is nowhere near where fsync-per-write becomes the bottleneck on the NFR-3 hardware tier (a modest SSD comfortably does thousands of small fsyncs/sec). The alternative the open question posed — making the disk WAL the *primary* ack path instead of a Redis failover — would mean every packet pays a local fsync AND a Redis round trip on the common case; `appendfsync always` gets the same guarantee (every acked write survives a hard kill) for one fsync, not two, and requires no application-level change at all.

**Action for Agent A (`deploy/compose.yml` — outside this agent's exclusive paths):** change the `redis` service's command from
```
["redis-server", "--appendonly", "yes", "--appendfsync", "everysec"]
```
to
```
["redis-server", "--appendonly", "yes", "--appendfsync", "always"]
```
Reproduction: `cd backend && go run ./test/chaos -scenario unclean-reboot -sessions 30 -rate 25 -duration 15s -kill-for 10s` against a Redis container started with each flag (see `backend/test/chaos/docker.go`'s `provisionRedis` — pass `-redis-fsync always` — flag added for exactly this comparison) shows the difference directly.

## What this does NOT fix

The `spilled`/`drained`/(when Redis is down) `received` **audit counters** — not the data — can separately under-report by a small amount if `hikrad-acct` is hard-killed less than ~1s after accepting a record during a Redis outage, before the next periodic Postgres counter flush. This is a distinct, lower-severity, already-documented gap (`internal/accounting/server.go`'s `runCounterFlusher` doc comment) found by the `spill-corruption` chaos scenario: the underlying `sessions`/`usage_points` rows and the `persisted`/`deduplicated`/`reaped` counters (now written durably in the same DB transaction as the data itself — see `counters.go`'s `bumpPersistedInTx`) are unaffected either way. Flagged as a follow-up, not blocking this decision.
