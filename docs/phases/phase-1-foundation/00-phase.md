# Phase 1 — Foundation & Contracts

> Goal: a running Docker Compose stack, the monorepo scaffold, frozen project-wide conventions, CI, and the first **static** RADIUS Access-Accept (FreeRADIUS → stub policy endpoint). No earlier gates — this is the start. Master phase P1. Baseline docs: [master PRD](../../PRD.md), sub-PRDs [01](../../prd/01-platform-install-licensing.md), [02](../../prd/02-radius-nas-aaa.md), [07](../../prd/07-subscriber-portal-pwa.md).

## Agent roster & path ownership (verified disjoint)

| Agent | Task file | Exclusive paths this phase |
|---|---|---|
| A — Platform & Security | [agent-1-platform-security.md](agent-1-platform-security.md) | `deploy/**` (except `deploy/freeradius/`), `scripts/**`, `.github/**`, `backend/internal/platform/**`, `docs/ops/**`, repo root files (`README.md`, `.gitignore`, `Makefile`), migrations `0010–0019` |
| B — RADIUS & NAS | [agent-2-radius-nas.md](agent-2-radius-nas.md) | `deploy/freeradius/**`, `backend/internal/radius/**`, `backend/test/harness/**` |
| D — Backend Business | [agent-3-backend-business.md](agent-3-backend-business.md) | `backend/go.mod`, `backend/cmd/hikrad-api/**`, `backend/internal/httpapi/**`, `backend/internal/subscribers/**`, `backend/internal/profiles/**`, `backend/internal/seed/**`, migrations `0001–0009` |
| E — Frontend Panel | [agent-4-frontend-panel.md](agent-4-frontend-panel.md) | `frontend/panel/**` |
| F — Frontend Portal & Localization | [agent-5-frontend-portal.md](agent-5-frontend-portal.md) | `frontend/shared/**`, `frontend/portal/**` |

## Frozen contracts (do not renegotiate mid-phase)

### C1. Repo layout
```
deploy/            compose.yml, caddy/, .env.example        (A; deploy/freeradius/ is B's)
scripts/           install.sh, hikrad (CLI wrapper)         (A)
backend/           single Go module github.com/hikrad/hikrad
  cmd/hikrad-api/  cmd/hikrad-acct/  cmd/hikrad-monitor/    (api→D; acct,monitor→C, Phase 2)
  internal/{platform,auth}/                                 (A)
  internal/{httpapi,subscribers,profiles,billing,portalapi,reports,seed}/   (D)
  internal/radius/                                          (B)
  internal/{accounting,live,monitorsvc}/                    (C, Phase 2)
  migrations/NNNN_slug.sql                                  (range-partitioned per agent)
  test/harness/ (B)   test/chaos/ (C, Phase 5)
frontend/panel/ (E)   frontend/portal/ (F)   frontend/shared/ (F)
```

### C2. API conventions (FR-52; sub-PRD 01 §FR-52.2)
- Base `/api/v1`; JSON; errors always `{"error":{"code":"string","message":"string","field_errors":[{"field","message"}]}}`; HTTP codes conventional (400/401/403/404/409/422/500).
- List endpoints: `?cursor=<opaque>&limit=<n≤100>` → `{"items":[…],"next_cursor":"…|null"}`.
- Times RFC 3339 UTC. Auth: `Authorization: Bearer <access-token>` (stubbed this phase, real in Phase 2).

### C3. Module registration (avoids shared-file edits forever)
`internal/httpapi` exposes `type Module interface { Name() string; Register(r chi.Router, d Deps) }` and a package-level `httpapi.Add(m Module)` registry called from each domain package's `init()`. `cmd/hikrad-api/modules.go` (owned by D) contains only blank imports of all planned packages: platform, auth, radius, subscribers, profiles, billing, portalapi, reports, live — written **now**, so future phases add no shared-file edits. `Deps` = `{DB *pgxpool.Pool, Redis *redis.Client, Settings platform.Settings, Log *slog.Logger}`.

### C4. Authorize endpoint (stub this phase; full policy Phase 2)
`POST /internal/radius/authorize` (served by hikrad-api on the internal port 8080, not proxied by Caddy):
Request `{"username","password":?, "chap_challenge":?, "chap_response":?, "nas_ip","calling_station_id":?, "service":"pppoe|hotspot"}`
Response `{"action":"accept|reject","reason":"ok|bad_password|expired|disabled|session_limit|mac_mismatch|unknown_user|unknown_nas","attributes":[{"intent":"rate_limit|address_pool|session_timeout","value":"string"}]}`
Phase-1 stub (B implements inside `internal/radius`): accept exactly the seeded `testuser`/`testpass` with `rate_limit "10M/10M"`, reject everything else. Vendor mapping of intents→VSAs happens FreeRADIUS-side per B's config (Phase 1 hardcodes Mikrotik-Rate-Limit for the stub intents).

### C5. Environment & service topology (A owns `.env.example`; everyone codes against it)
Services on the Compose network: `postgres:5432`, `redis:6379`, `hikrad-api:8080`, `freeradius` (1812/1813/3799 udp host-published), `caddy` (80/443 host-published → panel `/`, portal `/portal`, api `/api`). Env names: `HIKRAD_DB_URL`, `HIKRAD_REDIS_URL`, `HIKRAD_ENCRYPTION_KEY`, `HIKRAD_JWT_SECRET`, `HIKRAD_ENV=dev|prod`.

### C6. Phase-1 DB schema (D migrations 0001–0009; A 0010–0019)
- `0001_subscribers.sql`: id uuid pk, username citext unique, password_enc bytea, name, phone, status text default 'active', profile_id, expires_at timestamptz, created_at.
- `0002_profiles.sql`: id, name, price_iqd bigint, duration_days int, rate_down_kbps int, rate_up_kbps int, created_at.
- `0010_settings.sql` (A): key text pk, value jsonb, updated_at. `0011_managers.sql` (A): id, username unique, password_hash, role text default 'admin', created_at.
Later columns arrive in later phases — do not pre-add.

### C7. Dev auth stub (so E can build login now)
`POST /api/v1/auth/login {"username","password"}` → `{"access_token","refresh_token","manager":{"id","username","role"}}`. Phase 1: implemented by D as a dev-mode stub validating against seeded `admin`/`admin` (only when `HIKRAD_ENV=dev`). Phase 2 replaces internals (A) with the same shape.

### C8. i18n contract (NFR-6; sub-PRD 07)
`frontend/shared` is a workspace package `@hikrad/shared` exporting: `I18nProvider`, `useT()` (namespaced keys `"module.key"`), `useLocale()` (`en|ar|ku`, `dir` auto), `formatIQD()`, `formatDate()`. Locale JSON in `frontend/shared/locales/{en,ar,ku}/*.json`. Rule frozen for all UI work in all phases: **no hardcoded user-visible strings**; CI check `npm run i18n:check` (F builds it) fails on violations. All layout via CSS logical properties; `dir` set on `<html>`.

### C9. Toolchain
Go 1.22+, chi router, pgx, golang-migrate (files), React 18 + Vite + TypeScript strict, npm workspaces at `frontend/`. Panel/portal build to static dists served by Caddy.

## Cross-assignments (deliberate duplicates)

FR-49 split: A owns install/compose; the *wizard UI* is Phase 5 (E). FR-52 backend framework is D; consumed by all.

## Integration gate (close Phase 1 when all pass)

1. `docker compose up` from a clean checkout → all services healthy (`postgres`, `redis`, `hikrad-api`, `freeradius`, `caddy`).
2. `make seed` loads demo data (admin manager, testuser subscriber, one profile).
3. B's packet harness sends PPPoE-style Access-Request for `testuser` → **Access-Accept with Mikrotik-Rate-Limit** through real FreeRADIUS → rlm_rest → stub (C4); wrong password → Access-Reject.
4. Panel served at `https://localhost/` — login with seeded admin (stub C7) reaches an empty dashboard shell; portal skeleton at `/portal` renders in all three locales with correct RTL flip.
5. CI green on a PR: Go build+test, frontend build+lint, `i18n:check`, harness smoke test.
6. `docs/ops/dev-setup.md` lets a new machine reproduce 1–5.
