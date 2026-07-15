# NFR-1 perf suite (Phase 5, Agent 2)

Five tools, one per NFR-1 number in the phase brief:

| Tool | Measures | Needs |
|---|---|---|
| `ingest/` | Sustained (7 pkt/s) + burst (50 pkt/s) accounting ingest: steady-state queue depth, ack latency p50/p95/p99 | Only Postgres+Redis+hikrad-acct (standalone, self-provisioning like `test/chaos`) |
| `authload/` | RADIUS auth p50/p95/p99 at 50 req/s burst (the headline NFR-1 gate) | A running FreeRADIUS+hikrad-api stack |
| `sse/` | Packet-to-screen latency (Accounting-Start → visible on `GET /api/v1/live/sessions`) | hikrad-acct + hikrad-api + a manager bearer token |
| `panelapi/` | p95 on the heavy panel endpoints (dashboard, subscriber list, usage graphs) | A running hikrad-api + a manager bearer token |
| `sizing/` | NFR-3: bulk-generates synthetic `usage_points`, measures real compression ratio, extrapolates to the 12-month/2000-session tier, checks retention-policy config | Only Postgres (standalone, self-provisioning) |
| `resources/` | Peak CPU%/memory per container during a load run, against the 4 vCPU/8 GB budget | `docker stats` access to the containers under test |

`ingest/` and `sizing/` are fully self-contained (they provision their own
throwaway Postgres/Redis, same approach as `test/chaos` — see its README for
why) and were actually run in this sandbox; `authload/`, `sse/`, and
`panelapi/` need a full FreeRADIUS+hikrad-api+panel stack and were **not**
exercised live here (see `docs/evidence/README.md` for why, and what to run
at the pilot).

## Running

```sh
cd backend

# Ingest queue-depth + latency (self-provisioning, CI-nightly scale):
go run ./test/perf/ingest -sessions 200 -sustained-for 30s -burst-for 15s

# Full NFR-1 scale:
go run ./test/perf/ingest -sessions 2000 -sustained-for 5m -burst-for 1m

# Sizing (self-provisioning; -months 12 -sessions 2000 for the full evidence run):
go run ./test/perf/sizing -months 1 -sessions 200

# Against a live compose stack (requires FreeRADIUS/hikrad-api/panel up and a
# registered NAS/subscriber, or a manager token respectively):
go run ./test/perf/authload -addr 127.0.0.1:1812 -rate 50 -duration 1m
go run ./test/perf/sse -token "$MGR_TOKEN" -samples 20
go run ./test/perf/panelapi -token "$MGR_TOKEN" -requests 30

go run ./test/perf/resources -containers hikrad-acct,hikrad-api,postgres,redis -duration 2m
```

Every tool writes a JSON report to `docs/evidence/raw/` and exits non-zero on
its own NFR-1/NFR-3 gate failing; `docs/evidence/generate.sh` folds the JSON
into the dated evidence report.
