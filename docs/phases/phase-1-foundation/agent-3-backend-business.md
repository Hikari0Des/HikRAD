# Phase 1 — Agent 3 (Backend Business): Go module, /api/v1 framework, core schema, seed

> Owns FR-52; depends on contracts in [00-phase.md](00-phase.md) (C1, C2, C3, C6, C7); parallel with Agents 1, 2, 4, 5.

## Mission & context
All HikRAD frontends talk to one Go service, `hikrad-api`, through a versioned REST API (`/api/v1`) — internal-only in v1 but built so Phase-2 mobile/public exposure needs no rework. You create the Go module, the HTTP framework every domain package plugs into, the first schema migrations, and seeded demo data. Detail source: sub-PRD [01-platform-install-licensing](../../prd/01-platform-install-licensing.md) §FR-52.

## File ownership
- **Exclusive:** `backend/go.mod`/`go.sum`, `backend/cmd/hikrad-api/**` (incl. `modules.go` with the frozen blank-import list from C3), `backend/internal/httpapi/**`, `backend/internal/subscribers/**` (placeholder pkg), `backend/internal/profiles/**` (placeholder pkg), `backend/internal/seed/**`, `backend/migrations/0001_*.sql`–`0009_*.sql`.
- **Read-only:** `backend/internal/platform` (Agent 1's constructors for Deps). **Forbidden:** `deploy/**`, `backend/internal/radius/**`, `frontend/**`.

## Tasks
1. `backend/go.mod` (`github.com/hikrad/hikrad`, Go 1.22), chi + pgx + golang-migrate deps pinned. [C9]
2. `internal/httpapi/`: router construction, the `Module`/`Add(Module)` registry and `Deps` struct exactly per C3; middleware chain: request ID, structured logging (slog), recovery, CORS (dev), content-type enforcement; the C2 error envelope helpers (`httpapi.Error(code, msg, fields…)`), cursor-pagination helpers (opaque base64 cursor over keyset), validation helper (struct tags → `field_errors`). [FR-52.1, FR-52.2]
3. `cmd/hikrad-api/main.go`: config via platform loader → run migrations → build Deps → mount registered modules under `/api/v1` (public, via Caddy) and `/internal` (unproxied) → serve :8080 with graceful shutdown. `modules.go`: blank imports per C3 (packages that don't exist yet get their placeholder created by their owners in later phases — import only what exists this phase: platform, radius, subscribers, profiles).
4. Migrations `0001_subscribers.sql`, `0002_profiles.sql` exactly per C6 (no extra columns — later phases extend).
5. `internal/subscribers` + `internal/profiles`: placeholder modules registering `GET /api/v1/{subscribers,profiles}` returning seeded rows with pagination — proves the framework end to end (full CRUD is Phase 2, same agent).
6. `internal/seed/`: `make seed` target loads dev fixtures — manager `admin/admin` (into Agent 1's `managers` table — write via SQL, don't import their package), subscriber `testuser`/`testpass` (password stored with AES-GCM using `HIKRAD_ENCRYPTION_KEY` — this encryption helper lives here temporarily and moves to Agent A's crypto service in Phase 2; document that), one profile "Basic 10M / 30d / 25000 IQD".
7. Dev auth stub per C7: `POST /api/v1/auth/login` validating seeded admin, issuing JWTs signed with `HIKRAD_JWT_SECRET` (claims: manager id, role) — gated to `HIKRAD_ENV=dev`; a `RequireAuth` middleware stub that only checks signature (real permissions arrive Phase 2 from Agent A — keep the middleware in `httpapi` as an injectable interface so A can swap the implementation without touching your files).
8. OpenAPI generation wiring (e.g. swaggo or hand-maintained `openapi.yaml` under `internal/httpapi/`) — internal artifact, not published. [FR-52.3]

Edge cases: pagination must be stable under concurrent inserts (keyset, not offset); error envelope must catch panics as 500s with request ID; `/internal/*` must be unreachable through Caddy (verify in test).

## Contracts consumed/exposed
- **Consumes:** C5 env/ports, platform config/migrate/Settings constructors, C6 schema.
- **Exposes:** the `httpapi` framework + registry (every backend agent), C2 conventions (both frontends), seeded fixtures (harness, panel login), auth-middleware injection seam (Agent A Phase 2).

## Definition of done
- Gate items 2 and 4 (API side): `make seed` works; login stub returns tokens; `GET /api/v1/subscribers` returns the seeded user with envelope+pagination; `/internal/radius/authorize` route mounts Agent 2's module.
- Unit tests: envelope, pagination cursor round-trip, validation → field_errors, auth stub accept/reject. Integration test: boot server against test DB, hit seeded endpoints.
- `go vet` + tests green in CI.

## Handoff
Phase 2 receives: the framework all domain modules plug into, working seed/demo data, the auth-middleware seam for Agent A, and placeholder subscribers/profiles packages this same role fills out with FR-1–12.
