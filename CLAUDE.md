# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this project is

HikRAD is a commercial RADIUS AAA + billing platform for Iraqi ISPs (a Snono SAS4 / SAMM alternative), sold as a one-time license and installed on-premise. Its differentiator is monitoring: real-time session visibility and a **lossless accounting pipeline** — "never lose an Accounting-Request" is the core product claim (success metric M2) and drives most architectural decisions.

**Current state:** v1, v2, and v3 are shipped. v4 (a structural program: native install replacing Docker, licensing v2, a router manager, double-entry accounting, portal rebuild — see [docs/v4/00-v4-master-plan.md](docs/v4/00-v4-master-plan.md)) is in progress. **What shipped when, with FRs/migrations/gate links, is in [docs/history.md](docs/history.md) — read that before assuming a feature doesn't exist.** **Every bug found gets a row in [docs/ops/known-issues.md](docs/ops/known-issues.md)** — check it before debugging anything; it has an Open/Closed/Wontfix status and, for open rows, the phase that owns the fix.

## Document hierarchy (order of truth)

1. [docs/PRD.md](docs/PRD.md) — master PRD, the source of truth. All Decisions Log entries are user-confirmed; do not contradict them.
2. [docs/prd/](docs/prd/00-index.md) — 8 domain sub-PRDs elaborating the master (requirement ownership, acceptance criteria, API/data contracts per domain). If a sub-PRD disagrees with the master, the master wins — fix the sub-PRD.
3. [docs/v4/](docs/v4/00-v4-master-plan.md) — the active program plan and per-phase briefs (`phases/phase-v4-N-*/00-phase.md`). Each phase brief has **frozen contracts** (API shapes, schema, events) and an integration gate — read the brief for the area you're touching before changing its contracts; they're amended explicitly, never silently.
4. [docs/history.md](docs/history.md) — what shipped in v1/v2/v3, one row per phase, linking to `docs/history/gates/<phase-id>.md` for full evidence. `docs/phases/00-team.md` is the (historical-only) v1 multi-agent execution model; all v2+ work is solo/sequential (one Claude Code session per phase, PRD Decision 25).

Requirement IDs (FR-N, NFR-N) are used everywhere; trace any implementation work back to them. Every FR is owned by exactly one sub-PRD (mapping in [docs/prd/00-index.md](docs/prd/00-index.md)). New owner requests land as a new v4 phase brief or inside an unstarted phase's brief — never interleaved into a running phase.

**Migrations are one linear sequence** (`backend/migrations/`): golang-migrate applies only versions greater than the database's current one, so **every new migration — maintenance or phase — takes the next free number above the repo's current maximum**. A lower number is silently skipped on updated installs. Per-phase migration ranges in a phase brief are budgets, not reservations that survive being passed.

## Commands

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
npm run build --workspaces --if-present   # tsc -b — also type-checks test files; this is the CI-authoritative build check, not tsc --noEmit
npm run test --workspaces --if-present
npm run i18n:check                        # CI-fatal: fails on hardcoded user-visible strings

cd frontend/panel
npm run dev                 # Vite dev server
npx vitest run src/path/to/File.test.tsx   # single test file
```

Repo-root `Makefile` and `scripts/hikrad` wrap the deploy stack (`make up`/`down`/`seed`/`test`/`migrate`/`lint`) — **v4-2 replaces the Docker Compose stack with a native systemd install; treat `deploy/compose.yml`/`deploy/docker/`/`deploy/caddy/` as scheduled for deletion in that phase, not as a stable target for new work.**

CI (`.github/workflows/ci.yml`) runs independent jobs guarding on whether their inputs exist yet: `backend` (go vet/build/test -race against real Postgres+Redis service containers), `frontend` (lint/build/i18n:check + `scripts/lint-no-native-controls.sh`), `scripts` (shell self-tests, install idempotency), `harness-smoke`.

## Architecture invariants

Go backend (single module `github.com/hikrad/hikrad`) · FreeRADIUS 3.2 · PostgreSQL 16 + TimescaleDB · Redis · React 18 + TypeScript. Three binaries: **`hikrad-api`** (`backend/cmd/hikrad-api/`) — REST API (`/api/v1`, chi router) for panel + portal, plus the FreeRADIUS policy endpoint `POST /internal/radius/authorize` (unproxied, sub-100ms p99, Redis-cached read-model); runs pending migrations on boot. **`hikrad-acct`** (`internal/accounting/`) — accounting ingest, acks 204 only after durable enqueue. **`hikrad-monitor`** — ICMP/SNMP NAS probes + alerts engine.

**Module registry** (`internal/httpapi`): every domain package self-registers instead of editing a shared route file (`Module` interface + `Add(m Module)` called from each package's `init()`). `backend/cmd/hikrad-api/modules.go` is blank imports only — mounting a new domain package means adding one import line, nothing else. `Deps = {DB *pgxpool.Pool, Redis *redis.Client, Settings platform.Settings, Log *slog.Logger}` is frozen.

**API conventions:** base `/api/v1`; JSON; errors always `{"error":{"code","message","field_errors":[{"field","message"}]}}`. List endpoints: `?cursor=<opaque>&limit=<n≤100>` → `{"items":[…],"next_cursor":"…|null"}`. Times RFC 3339 UTC. `Authorization: Bearer <access-token>`; permission strings are `<module>.<verb>` (e.g. `subscribers.view`), checked by string, never by role name; `auth.ScopeFilter(ctx)` applies per-manager scoping; `auth.Audit(...)` is called by every mutating endpoint.

**RADIUS policy engine** (`internal/radius`): consults `subscribers.AuthView` (Redis-cached; `InvalidatePolicy`/`InvalidatePolicyByProfile` invalidate on mutation), handles PAP+CHAP, MAC lock, session limits, expiry behavior, quota behavior, and the `service_type ∈ pppoe|hotspot|dual` matrix. A NAS runs many service instances (`nas_services`); every Access-Request resolves to one via `vendor.ResolveService` before the subscriber lookup — no match rejects `nas_not_allowed`. Subscribers/profiles may be scoped to a NAS/instance (subscriber-over-profile, checked before credentials). Reply pools are service-aware. **Vendor neutrality:** RADIUS reply attributes are abstract intents (`rate_limit`, `address_pool`, `session_timeout`, `address_list`); MikroTik VSA mapping happens only in `internal/radius/vendor/` — CI greps for violations. **Router-ownership principle** (binding on all HikRAD↔router config, generalized from `nas_services.management_mode`): HikRAD may create/edit/remove/push only objects it owns (`management_mode = system`); router-adopted objects (`= router`) are read-only references, never written back, and nothing is ever deleted from a router. NAS auto-discovery via MNDP is read-only.

**Money and audit tables:** append-only (DB-level `REVOKE UPDATE/DELETE`); balances are always derived from the ledger, never stored-and-edited directly. Single IQD ledger book (post v3-2 — do not reintroduce per-currency wallets); foreign-currency entries convert to IQD at entry time and are stamped with the original amount/currency/rate for display, never re-converted on refund. `audit_log` follows the same append-only pattern.

**Crypto:** subscriber RADIUS passwords are reversible-encrypted (AES-GCM, `HIKRAD_ENCRYPTION_KEY`, `internal/platform/crypto`) because CHAP requires cleartext at auth time; decryption happens only inside the RADIUS authorize path. The same service encrypts NAS secrets/SNMP community strings.

**Frontend:** `frontend/panel` (admin) and `frontend/portal` (subscriber) both consume `frontend/shared` (`@hikrad/shared`, a source package) for i18n (`I18nProvider`, `useT()`, `useLocale()`, `formatMoney()`, `formatDate()`) and a thin API client. Trilingual (en/ar/ku), true RTL, charts/usernames/MACs/IPs stay LTR inside RTL. **No hardcoded user-visible strings** (`npm run i18n:check` is CI-fatal) and **no native form controls** — use `frontend/{panel,portal}/src/components/form/` (Radix-based Select/Combobox/Checkbox/Radio/Switch/FileInput/TextInput/Textarea/FormField), enforced by `scripts/lint-no-native-controls.sh`. `FormField` associates its label by construction — prefer it over the older `Field` for any new form.

**Availability:** nothing required for daily operation may depend on internet access (NFR-7) — license validation is offline; online-dependent features must degrade gracefully.

## Domain context worth knowing

- Personas gauge every UX decision: Sara (front desk, low technical, ≤3 clicks), Omar (owner, dashboard-on-phone), Ali (network engineer, MikroTik expert), Hassan (field agent, phone-first, balance-driven), Noor (subscriber, portal).
- Timezone Asia/Baghdad, currency IQD; Arabic text handling (including CP1256 in CSV imports) is a first-class requirement, not an edge case.
- "Expired" subscribers are usually not cut off — they're moved to a walled-garden IP pool with a renewal redirect, and renewal restores full speed via CoA without re-dialing. This renew→CoA path is the product's hero flow.
