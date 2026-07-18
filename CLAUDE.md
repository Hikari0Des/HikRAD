# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this project is

HikRAD is a commercial RADIUS AAA + billing platform for Iraqi ISPs (a Snono SAS4 alternative), sold as a one-time license and installed on-premise via Docker. Its differentiator is monitoring: real-time session visibility and a **lossless accounting pipeline** — "never lose an Accounting-Request" is the core product claim (success metric M2) and drives most architectural decisions.

**Current state: v1 cut (Phases 1–5 complete) + v1.1 maintenance pass (2026-07-16).** The v1.1 pass (owner's 26-item review) fixed the voucher "code doesn't look right" bug (separator normalization), permission-denied page UX (route guards + `noAccess` page), live balance refresh, MNDP discovery under Docker (UDP 5678 now published), and added: NAS RouterOS-version probe (`POST /nas/{id}/probe`), passwordless hotspot subscribers (`no_password`/`has_password`, migration 0412), `hikrad uninstall` + `scripts/uninstall.sh`, dark/light/system themes (`@hikrad/shared` theme store + `data-theme` tokens), Settings > System guided-update screen (version via `HIKRAD_VERSION` ldflag from the repo-root `VERSION` file), voucher code-length option, portal name self-edit. A second v1.x pass (2026-07-17, pulled forward from the v3 backlog) added: manager disable + guarded hard delete (migration 0505, `DELETE /managers/{id}`; ledger-history managers map to "disable instead"), `hikrad factory-reset` (wipe all data, keep the install), themed slim scrollbars, and NAS/device status pages titled by name instead of uuid. **Every bug found from now on gets a row in [docs/ops/known-issues.md](docs/ops/known-issues.md)** — check it before debugging anything. **v2 is in progress**: briefs + kickoff prompts in [docs/v2/](docs/v2/00-v2-index.md), sequential SOLO single-agent phases (no parallel teams — owner decision 2026-07-16) in [docs/v2/phases/00-v2-execution-plan.md](docs/v2/phases/00-v2-execution-plan.md); migrations are one linear sequence — always the next free number (see below). **v2 phase 1 (hotspot management, FR-61–64) is complete** — subscriber `service_type` (pppoe/hotspot/dual) replacing `allow_hotspot`, multi-service NAS via `nas_services` (`nas.type` retired), subscriber/profile→NAS scoping enforced at auth, and service-aware reply pools; gate GREEN 9/9, see [docs/v2/phases/phase-v2-1-hotspot-management/gate-result.md](docs/v2/phases/phase-v2-1-hotspot-management/gate-result.md). **v2 phase 3 (NAS auto-setup config manager + Hotspot/PPPoE server management, FR-65–67) is complete** — `GET /nas/{id}/config` read-only inspection, form-driven auto-setup values with per-conflict keep/update/abort resolution (abort stays default, additive to Decision 17), and `nas_services.management_mode` (`router`/`system`) with create/edit/adopt server-provisioning endpoints, all through the same hash-gated preview/apply pattern; migration 0520; gate GREEN 27/27 scripted legs, see [docs/v2/phases/phase-v2-3-autosetup-config-manager/gate-result.md](docs/v2/phases/phase-v2-3-autosetup-config-manager/gate-result.md). **v2 phase 4 (multi-currency billing, FR-68–70) is complete** — `currencies` catalog (IQD/USD/EUR), admin-entered-only `currency_rates` (no online rate feed, AC-68b), per-`(manager_id, currency)` balances, explicit exchange as the only conversion path, renewals/vouchers/refunds/receipts/reports all threaded with currency (a refund always reverses in its *original* entry's currency, never re-resolved, FR-69.5), `formatMoney` generalizing `formatIQD` in `@hikrad/shared` (byte-identical regression-locked for IQD), and a panel `/currency-rates` screen (rate table + exchange); migrations 0530–0538; gate GREEN 10/10, see [docs/v2/phases/phase-v2-4-multi-currency/gate-result.md](docs/v2/phases/phase-v2-4-multi-currency/gate-result.md). **v2-9 (cost/margin/reseller pricing, FR-71–76) is complete** — versioned plan cost (`profile_cost_history`, query-don't-mirror like `currency_rates`), margin derived on the ledger (`cost_at_sale`, never stored), global/per-site overheads (never pro-rated across each other), flat 2-level reseller wholesale pricing with optional per-subscriber overrides, and a commercial-severity reseller-scoping contract (cost/wholesale data omitted — not nulled — from a reseller's own API responses); migrations 0540–0543; gate GREEN 11/11, see [docs/v2/phases/phase-v2-9-cost-margin-pricing/gate-result.md](docs/v2/phases/phase-v2-9-cost-margin-pricing/gate-result.md). **v2-2 (manual payment providers, FR-77–80) is complete** — retires the Phase-4 e-wallet gateway surface entirely (`PaymentGateway`, mock/zaincash adapters, `payment_intents`/`gateway_configs`, kickoff blocker 2) in favor of named per-manager receiving accounts + a unified `payment_tickets` table generalizing FR-59's scratch-card flow to every method (provider transfer-proof with attachments, scratch card), reusing the exact FR-59.1 trial mechanism and threading v2-9's wholesale/retail split into approval; a subscriber only ever sees a payment method their owning manager has both enabled AND configured an account for, with no fallback to any other account anywhere (kickoff blocker 1); one portal Pay screen replaces the old gateway list + separate scratch-card screen; migrations 0580–0589 (0586 losslessly migrates `card_payments`→`payment_tickets`, 0587 drops the gateway tables, 0588 backfills voucher/scratch-card enablement for every pre-existing manager so the upgrade doesn't silently disable them); gate GREEN 12/12, see [docs/v2/phases/phase-v2-2-manual-payments/gate-result.md](docs/v2/phases/phase-v2-2-manual-payments/gate-result.md). **v2-6 (per-manager preferences) is next** per the execution plan. (**Note on labels 2026-07-17:** the `v2-N` phase numbers were corrected to match their source file's number (`docs/v2/0N-*.md`) — e.g. this NAS auto-setup phase is file `03` so it is `v2-3`, not the `v2-2` label used while it was being built; see PRD Decision 35 and `docs/v2/phases/00-v2-execution-plan.md` for the full corrected mapping and the actual build order, which is unchanged.) (Compose stack, real RADIUS policy engine, manager auth, subscribers/profiles, lossless accounting pipeline, live sessions, panel screens for all of the above; billing/ledger with renewals+vouchers+receipts+refunds, roles/2FA/audit, runtime enforcement, NAS/device monitoring + alerts, dashboard; subscriber portal with e-wallet + scratch-card payments, PWA packaging for panel+portal, panel web push, subscriber WhatsApp messaging, NAS API auto-setup, ROS 6/7 quirk matrix; reports reconciling to the ledger, SAS4 CSV migration, offline licensing with grace/expired-grace, backup/restore/update CLI, optional Cloudflare tunnel, ASVS L2 pass, scripted chaos+perf evidence pack). Phase 3 integration gate passed 2026-07-11 (`docs/phases/phase-3-billing-security-monitoring/gate-result.md`) — GREEN, 8/8 gate items PASS. Phase 4 integration gate passed 2026-07-12 (`docs/phases/phase-4-portal-payments-pwa/gate-result.md`) — GREEN, 10/10 gate items PASS (hardware/merchant-account/Meta-onboarding halves of items 1/3/4/5/7/8/9 documented-pending per the phase brief's own sanctioned fallback). Phase 5 integration gate passed 2026-07-16 (`docs/phases/phase-5-v1-reports-install-license/gate-result.md`) — GREEN, 8/8 gate items PASS (item 3's full restore-round-trip/update-rollback and item 8's live Cloudflare edge are the residual documented-pending pieces; see that gate result and `docs/ops/release-checklist.md`). Historical bugs found and fixed during the v1 phases are indexed in [docs/ops/known-issues.md](docs/ops/known-issues.md).

## Document hierarchy (order of truth)

1. [docs/PRD.md](docs/PRD.md) — master PRD, the source of truth. All decisions in its Decisions Log are user-confirmed; do not contradict them.
2. [docs/prd/](docs/prd/00-index.md) — 8 domain sub-PRDs elaborating the master (requirement ownership, acceptance criteria, API/data contracts per domain). If a sub-PRD disagrees with the master, the master wins — fix the sub-PRD.
3. [docs/phases/](docs/phases/00-team.md) — the **v1** multi-agent execution plan (6 agent roles, 5 phases, complete). Each phase's `00-phase.md` contains **frozen contracts** (API shapes, schema, events) and an integration gate. Read the phase brief for the area you're touching before changing its contracts — they are frozen and amended explicitly, never silently. **v2 phases live under [docs/v2/phases/](docs/v2/phases/00-v2-execution-plan.md) and are executed solo and sequentially.**

Requirement IDs (FR-1…FR-60, NFR-1…NFR-8) are used everywhere; trace any implementation work back to them. Every FR is owned by exactly one sub-PRD (mapping in [docs/prd/00-index.md](docs/prd/00-index.md)). Deferred v2 features live as briefs + AI kickoff prompts in [docs/v2/](docs/v2/00-v2-index.md) — the **only** backlog (a short-lived docs/v3 parking list was merged back into v2 phases v2-10/11/12 and deleted, 2026-07-17, PRD Decision 32); new owner requests land there as a new brief or inside an unstarted phase's brief, never interleaved into a running phase. The multi-agent execution rules include a binding token-efficiency protocol ([docs/phases/00-team.md](docs/phases/00-team.md) §Execution-efficiency).

## Agent path ownership (v1 historical reference)

**v2+ work is SOLO and sequential** — the role split below is no longer an execution model, only a map of who built what during v1 (useful for finding the right phase brief/gate result). v1's plan divided work into 6 roles so parallel agents wouldn't collide:

| Area | Owner role | Key paths |
|---|---|---|
| Platform & Security | A | `deploy/**` (exc. `deploy/freeradius/`), `scripts/`, `.github/`, `backend/internal/platform/**`, `backend/internal/auth/**`, `docs/ops/` |
| RADIUS & NAS | B | `deploy/freeradius/**`, `backend/internal/radius/**`, `backend/test/harness/**` |
| Accounting & Monitoring | C | `backend/cmd/hikrad-acct/**`, `backend/internal/accounting/**`, `backend/internal/live/**` |
| Backend Business | D | `backend/cmd/hikrad-api/**`, `backend/internal/httpapi/**`, `backend/internal/subscribers/**`, `backend/internal/profiles/**`, `backend/internal/seed/**` (Phase 3+: `billing/`, `portalapi/`, `reports/`) |
| Frontend Panel | E | `frontend/panel/**` |
| Frontend Portal & Localization | F | `frontend/shared/**`, `frontend/portal/**` |

Migration files in `backend/migrations/` form **one linear sequence**: golang-migrate applies only versions greater than the database's current one, so **every new migration — maintenance or phase — must take the next free number above the repo's current maximum** (a lower number is silently skipped on updated installs; that near-miss is documented in [docs/ops/known-issues.md](docs/ops/known-issues.md), 2026-07-17). The per-phase ranges in [docs/v2/phases/00-v2-execution-plan.md](docs/v2/phases/00-v2-execution-plan.md) are budgets for how many numbers a phase may consume, not reservations that survive being passed. History: v1 phases used 0001–0411, v1.x maintenance 0412 then 0505+, v2 phases 0500+.

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
Real authorize logic (Phase 2, reworked by v2 phase 1): consults `subscribers.AuthView` (Redis-cached read-model, `radius.InvalidatePolicy(subscriberID)` invalidates it on every subscriber/profile mutation; `InvalidatePolicyByProfile` fans a profile change out to its subscribers), handles PAP+CHAP, MAC lock (`off|learn|fixed`), session limits (counted via `internal/live`), expiry behavior (`block|expired_pool`), quota behavior, and the **FR-61 service-type matrix** (`service_type ∈ pppoe|hotspot|dual` — `dual` is v1's `allow_hotspot=true` and preserves FR-58 semantics exactly; a hotspot-only account's quota and session limit DO apply). A NAS runs **many service instances** (`nas_services`, FR-62): every Access-Request resolves to one via `vendor.ResolveService` before the subscriber lookup, and a request that resolves to none rejects `nas_not_allowed` (a NAS-config fact, distinct from `service_not_allowed`, which is an account fact). Subscribers/profiles may be **scoped to a NAS/instance** (FR-64, subscriber-over-profile, checked before credentials). **Reply pools are service-aware**: a hotspot session never inherits the profile's PPPoE pool (that was the pilot's "no more free addresses" bug — see [docs/ops/known-issues.md](docs/ops/known-issues.md)). **Vendor neutrality (FR-17): RADIUS reply attributes are abstract intents** (`rate_limit`, `address_pool`, `session_timeout`); MikroTik VSA mapping happens only in `internal/radius/vendor/` — CI greps for violations, never introduce vendor-specific attribute names elsewhere in `internal/radius`. The package also owns NAS CRUD, IP pools, CoA (`coa.Disconnect`/`ApplyRate`/`MovePool`), and read-only NAS auto-discovery via MNDP (`discover.go`) — discovery never writes to a router.

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
