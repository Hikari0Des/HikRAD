# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this project is

HikRAD is a commercial RADIUS AAA + billing platform for Iraqi ISPs (a Snono SAS4 alternative), sold as a one-time license and installed on-premise via Docker. Its differentiator is monitoring: real-time session visibility and a **lossless accounting pipeline** — "never lose an Accounting-Request" is the core product claim (success metric M2) and drives most architectural decisions.

**Current state: Phases 1 and 2 of the execution plan are complete** (Compose stack, real RADIUS policy engine, manager auth, subscribers/profiles, lossless accounting pipeline, live sessions, panel screens for all of the above). Phase 3 (billing/ledger, roles/2FA, alerts, dashboard) is next. Multiple agents may be working in this repo concurrently on different phases/domains — check `git status` before editing near a path-ownership boundary (see below).

## Document hierarchy (order of truth)

1. [docs/PRD.md](docs/PRD.md) — master PRD, the source of truth. All decisions in its Decisions Log are user-confirmed; do not contradict them.
2. [docs/prd/](docs/prd/00-index.md) — 8 domain sub-PRDs elaborating the master (requirement ownership, acceptance criteria, API/data contracts per domain). If a sub-PRD disagrees with the master, the master wins — fix the sub-PRD.
3. [docs/phases/](docs/phases/00-team.md) — multi-agent execution plan: 6 agent roles, 5 phases, one task PRD per agent per phase. Each phase's `00-phase.md` contains **frozen contracts** (API shapes, schema, events) and an integration gate. Read the phase brief for the area you're touching before changing its contracts — they are frozen for that phase and amended explicitly, never silently.

Requirement IDs (FR-1…FR-58, NFR-1…NFR-8) are used everywhere; trace any implementation work back to them. Every FR is owned by exactly one sub-PRD (mapping in [docs/prd/00-index.md](docs/prd/00-index.md)).

## Agent path ownership (for reference — respect even when working solo)

The plan divides work into 6 roles so parallel agents don't collide; task files declare exclusive path ownership per phase. Roughly, by current (Phase 2) layout:

| Area | Owner role | Key paths |
|---|---|---|
| Platform & Security | A | `deploy/**` (exc. `deploy/freeradius/`), `scripts/`, `.github/`, `backend/internal/platform/**`, `backend/internal/auth/**`, `docs/ops/` |
| RADIUS & NAS | B | `deploy/freeradius/**`, `backend/internal/radius/**`, `backend/test/harness/**` |
| Accounting & Monitoring | C | `backend/cmd/hikrad-acct/**`, `backend/internal/accounting/**`, `backend/internal/live/**` |
| Backend Business | D | `backend/cmd/hikrad-api/**`, `backend/internal/httpapi/**`, `backend/internal/subscribers/**`, `backend/internal/profiles/**`, `backend/internal/seed/**` (Phase 3+: `billing/`, `portalapi/`, `reports/`) |
| Frontend Panel | E | `frontend/panel/**` |
| Frontend Portal & Localization | F | `frontend/shared/**`, `frontend/portal/**` |

Migration files in `backend/migrations/` use numeric ranges assigned per agent per phase (e.g. Phase 2: A `0110–0119`, B `0120–0129`, C `0130–0139`, D `0100–0109`) — never take a number outside your assigned range; check the current phase brief for the live allocation.

## Commands

Repo-root `Makefile` (Docker Compose stack):
```sh
make up       # generate deploy/.env if missing, build and start the stack
make down     # stop the stack (data under deploy/data/ is kept)
make seed     # load demo data via `hikrad-api seed` (idempotent)
make test     # backend go test + scripts/gen-env.test.sh
make migrate  # apply pending DB migrations manually (they also run on api boot)
make lint     # go vet + frontend lint
```

Backend (Go, from `backend/`):
```sh
go build ./...
go vet ./...
go test ./...                       # unit tests; DB/Redis-backed tests self-skip if env is unset
go test ./internal/subscribers/...  # single package
go test ./internal/radius/ -run TestAuthorize_ExpiredPool   # single test

# Integration-style tests need a real Postgres/Redis and are gated on env vars
# they check for themselves — set these to opt in, unset to skip:
HIKRAD_TEST_DB_URL=postgres://hikrad:hikrad@localhost:5432/hikrad_test?sslmode=disable \
HIKRAD_TEST_REDIS_URL=redis://localhost:6379/0 \
  go test ./...

make -C backend test-harness-smoke  # brings up postgres/redis/hikrad-api/freeradius, seeds, runs the packet harness
```

RADIUS packet harness (`backend/test/harness/`, see its README) simulates a MikroTik NAS:
```sh
cd backend
go run ./test/harness -addr 127.0.0.1:1812 -secret testing123          # 5-case smoke suite (PAP/CHAP accept+reject)
go run ./test/harness -rate 50 -duration 30s                            # sustained load mode (NFR-1 perf check)
go run ./test/harness -mode mndp-announce -duration 8s                  # simulate NAS auto-discovery broadcast
go run ./test/harness -mode coa-listen -addr 127.0.0.1:3799 -secret X   # simulate NAS-side CoA/Disconnect receiver
```

Frontend (npm workspaces rooted at `frontend/`: `shared`, `panel`, `portal`):
```sh
cd frontend
npm ci
npm run lint --workspaces --if-present
npm run build --workspaces --if-present
npm run test --workspaces --if-present
npm run i18n:check          # CI-fatal: fails on hardcoded user-visible strings

cd frontend/panel
npm run dev                 # Vite dev server
npx vitest run src/path/to/File.test.tsx   # single test file
```

CI (`.github/workflows/ci.yml`) runs four independent jobs, each guarding on whether its inputs exist yet: `backend` (go vet/build/test -race against real Postgres+Redis service containers), `frontend` (lint/build/i18n:check), `scripts` (shell self-tests, `install.sh` idempotency), `harness-smoke` (`make -C backend test-harness-smoke`).

## Planned architecture (fixed by PRD Decision 11)

Go backend (single module `github.com/hikrad/hikrad`) · FreeRADIUS 3.2 · PostgreSQL 16 + TimescaleDB · Redis · React 18 + TypeScript · Docker Compose, three binaries:

- **`hikrad-api`** (`backend/cmd/hikrad-api/`) — REST API (`/api/v1`, chi router) for panel + portal, plus the FreeRADIUS policy endpoint `POST /internal/radius/authorize` (unproxied, sub-100 ms p99 budget, Redis-cached read-model). On boot it runs pending migrations (`platform.Migrate`), then builds `httpapi.Deps{DB, Redis, Settings, Log}` and serves `httpapi.NewRouter`. `hikrad-api seed` applies migrations + loads dev fixtures then exits.
- **`hikrad-acct`** (`backend/cmd/hikrad-acct/`, `internal/accounting/`) — accounting ingest; FreeRADIUS forwards each Accounting-Request via `rlm_rest` to `POST http://hikrad-acct:8082/acct`, and the ingest **acks 204 only after durable enqueue** (Redis stream `acct:stream` + disk spill under `data/acct-spill/`). A consumer dedups via `acct_dedup` (key: nas_id, acct_session_id, record_type, event_time) and upserts `sessions`/`usage_points` (TimescaleDB hypertable). `pipeline_counters` must always satisfy `received − duplicates − in_queue = persisted`; a reaper (`reaper.go`) synthesizes a Stop for sessions that go silent. Live state is mirrored into the Redis hash `live:sessions`, read by `internal/live/` and pushed to the panel over SSE (`GET /api/v1/live/sessions`).
- **`hikrad-monitor`** (not yet built — Phase 3) — ICMP/SNMP NAS probes + alerts engine (in-app/Telegram/SMTP/WhatsApp).

### Module registry (`internal/httpapi`)

Every domain package self-registers its HTTP module instead of editing a shared route file:
```go
type Module interface { Name() string; Register(r chi.Router, d Deps) }
func Add(m Module)  // called from each package's init()
```
`backend/cmd/hikrad-api/modules.go` contains only blank imports of every mounted package (`auth`, `platform`, `profiles`, `live`, `radius`, `subscribers`; Phase 3+ uncomments `billing`, `portalapi`, `reports` as they land) — mounting a new domain package means adding one import line here, nothing else. `Deps = {DB *pgxpool.Pool, Redis *redis.Client, Settings platform.Settings, Log *slog.Logger}` (frozen shape).

### API conventions (frozen)
- Base `/api/v1`; JSON; errors always `{"error":{"code","message","field_errors":[{"field","message"}]}}`; conventional HTTP codes.
- List endpoints: `?cursor=<opaque>&limit=<n≤100>` → `{"items":[…],"next_cursor":"…|null"}`.
- Times RFC 3339 UTC. Auth: `Authorization: Bearer <access-token>` (real JWT auth since Phase 2, `internal/auth`; middleware puts `auth.Manager` in request context; permission strings are `<module>.<verb>`, e.g. `subscribers.view`, checked by string — never by role name; `auth.ScopeFilter(ctx)` applies per-manager data scoping; `auth.Audit(...)` is called by every mutating endpoint into the append-only `audit_log`).

### RADIUS policy engine (`internal/radius`)
Real authorize logic (Phase 2): consults `subscribers.AuthView` (Redis-cached read-model, `radius.InvalidatePolicy(subscriberID)` invalidates it on every subscriber/profile mutation), handles PAP+CHAP, MAC lock (`off|learn|fixed`), session limits (counted via `internal/live`), expiry behavior (`block|expired_pool`), quota behavior, and dual-service (PPPoE vs Hotspot, FR-58) rules. **Vendor neutrality (FR-17): RADIUS reply attributes are abstract intents** (`rate_limit`, `address_pool`, `session_timeout`); MikroTik VSA mapping happens only in `internal/radius/vendor/` — CI greps for violations, never introduce vendor-specific attribute names elsewhere in `internal/radius`. The package also owns NAS CRUD, IP pools, CoA (`coa.Disconnect`/`ApplyRate`/`MovePool`), and read-only NAS auto-discovery via MNDP (`discover.go`) — discovery never writes to a router.

### Money and audit tables
Append-only (DB-level `REVOKE UPDATE/DELETE`); balances/ledgers (Phase 3+) are always derived from the ledger, never stored-and-edited directly. `audit_log` follows the same append-only pattern.

### Crypto
Subscriber RADIUS passwords are reversible-encrypted (AES-GCM, `HIKRAD_ENCRYPTION_KEY`, `internal/platform/crypto`) because CHAP requires cleartext at auth time; decryption happens **only** inside the RADIUS authorize path (NFR-4.2). The same service encrypts NAS secrets/SNMP community strings.

### Frontend
`frontend/panel` (admin, Tailwind + Radix UI, CSS logical properties for RTL) and `frontend/portal` (subscriber) both consume `frontend/shared` (`@hikrad/shared`, a source package — Vite compiles it directly) for i18n (`I18nProvider`, `useT()`, `useLocale()`, `formatIQD()`, `formatDate()`) and a thin API client. Trilingual (en/ar/ku), true RTL, charts/usernames/MACs/IPs stay LTR inside RTL. **No hardcoded user-visible strings** — locale JSON lives at `frontend/shared/locales/{en,ar,ku}/<module>.json`; `npm run i18n:check` is CI-fatal.

### Availability
Nothing required for daily operation may depend on internet access (NFR-7): license validation is offline, e-wallet payments are the only online-dependent feature (Phase 4) and must degrade gracefully.

## Domain context worth knowing

- Personas gauge every UX decision: Sara (front desk, low technical, ≤ 3 clicks), Omar (owner, dashboard-on-phone), Ali (network engineer, MikroTik expert), Hassan (field agent, phone-first, balance-driven), Noor (subscriber, portal).
- Timezone Asia/Baghdad, currency IQD; Arabic text handling (including CP1256 in CSV imports) is a first-class requirement, not an edge case.
- "Expired" subscribers are usually not cut off — they're moved to a walled-garden IP pool with a renewal redirect, and renewal restores full speed via CoA without re-dialing (key flow 2). This renew→CoA path is the product's hero flow; the auth-time half (expired_pool behavior) is built, CoA-on-renewal enforcement lands in Phase 3.
