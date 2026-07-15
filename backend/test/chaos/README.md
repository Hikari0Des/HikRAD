# Accounting chaos suite (Phase 5, Agent 2 — FR-37.5, NFR-2)

Scripted, reproducible proof of HikRAD's M2 claim ("zero lost accounting
records") under the failures the phase brief names. Complements, and does not
replace, `internal/accounting/chaos_test.go`'s DB+Redis-gated code-level
suite (same guarantees, exercised in-process/unit-style — see that
package's README).

## What this rig is, and isn't

This tool drives a **real, freshly-built `hikrad-acct` binary** against
**real, disposable Postgres/Redis containers**, speaking the exact C6 ingest
wire shape (`POST /acct`) that FreeRADIUS's `rlm_rest` forward hits. It
deliberately does **not** stand up FreeRADIUS/Caddy/hikrad-api — B's
`deploy/freeradius/**` is unstaffed this phase and its Windows-host
bind-mount behavior is a documented, separate concern (see
`docs/evidence/README.md`). Testing at the `/acct` HTTP boundary exercises
100% of the lossless-pipeline code this suite is chartered to prove; the
FreeRADIUS→rlm_rest hop itself is proven by the harness's own smoke suite
(auth path) and is out of scope here.

Postgres/Redis run as standalone `docker run` containers with published
host ports and no bind-mounted config trees — this sidesteps the Windows
bind-mount permission issues that affect `deploy/freeradius/`/`caddy` (see
memory `hikrad-dev-environment`) entirely, so the same invocation works
identically on a dev laptop, CI, and the pilot's Linux host.

## Scenarios

| Name | Proves |
|---|---|
| `kill-postgres` | Consumer stalls, stream backlog grows, drains losslessly on recovery (FR-37.3) |
| `kill-redis` | Ingest fails over to the disk spill, drains on Redis recovery (FR-37.1) |
| `kill-acct` | Process SIGKILLed mid-flood; restart resumes the backlog (edge case in the brief) |
| `unclean-reboot` | Postgres+Redis+acct SIGKILLed **simultaneously**, no graceful shutdown anywhere |
| `retransmit-storm` | Each record delivered 3×; dedup counter absorbs exactly the 2× extra (AC-37b) |
| `out-of-order` | Interims delivered out of chronological order + one backdated; all land (FR-37.4) |
| `panel-down` | hikrad-api is never started at all; accounting is unaffected (AC-NFR2a, accounting half) |
| `spill-corruption` | One hand-corrupted WAL line spliced in; drain skips it, loses nothing else (checksum path) |
| `redis-durability` | Hard-kills Redis within its `appendfsync everysec` window; **measures**, doesn't gate, the loss (sub-PRD 03 §7 open question — see `docs/evidence/redis-durability-decision.md`) |

`reaper-vs-recovery-race` (a NAS returning while the reap timer is pending)
is already proven at the code level by `TestReaperLifecycle` in
`internal/accounting/chaos_test.go` — not duplicated here.

## Running it

```sh
cd backend
# CI-nightly / dev smoke (default flags: 200 sessions, 50 pkt/s, 20s flood):
go run ./test/chaos -scenario all

# Full-scale evidence-pack mode (matches NFR-1's 5k/2k reference load):
go run ./test/chaos -scenario all -sessions 2000 -rate 50 -duration 5m -kill-for 10m
```

Requires Docker (containers named `hikrad-chaos-postgres`/`hikrad-chaos-redis`
by default; pass `-provision=false` to reuse an already-running pair via
`-db-url`/`-redis-url`). Each scenario writes
`docs/evidence/raw/<scenario>.json`; `docs/evidence/generate.sh` folds these
into the dated evidence report (contract C6).

Exit code is non-zero if any scenario fails or errors.
