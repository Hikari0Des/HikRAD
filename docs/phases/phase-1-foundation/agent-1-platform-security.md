# Phase 1 — Agent 1 (Platform & Security): Compose stack, env, CI, settings skeleton

> Owns FR-49 (base), FR-53 (skeleton), NFR-3, NFR-7 groundwork, NFR-8 (CI). Depends on contracts in [00-phase.md](00-phase.md) (C1, C5, C6, C9); parallel with Agents 2–5.

## Mission & context
HikRAD is an on-prem, Docker-installed RADIUS+billing product for Iraqi ISPs (Go + FreeRADIUS + Postgres/Timescale + Redis + React, one server, 4 vCPU/8 GB/200 GB tier). You build the shell everything runs in: the Compose stack, secrets/env generation, migration tooling, CI, the settings service skeleton, and dev docs. Detail source: sub-PRD [01-platform-install-licensing](../../prd/01-platform-install-licensing.md).

## File ownership
- **Exclusive:** `deploy/**` (EXCEPT `deploy/freeradius/` — Agent 2's), `scripts/**`, `.github/**`, `backend/internal/platform/**`, `backend/migrations/0010_*.sql`–`0019_*.sql`, `docs/ops/**`, repo root (`README.md`, `.gitignore`, `Makefile`, `.editorconfig`).
- **Read-only:** everything else. **Forbidden:** `backend/internal/{radius,httpapi,subscribers,profiles}`, `frontend/**`.

## Tasks
1. Repo root: `README.md` (project one-pager + quickstart), `.gitignore`, `Makefile` (`up`, `down`, `seed`, `test`, `migrate`, `lint`). Initialize git if needed. [NFR-8]
2. `deploy/compose.yml` per contract C5: postgres (16 + timescaledb image), redis (AOF `appendfsync everysec`), hikrad-api, freeradius (image + mounts only — config content is Agent 2's dir), caddy. Healthchecks on all; restart policies `unless-stopped`; named volumes under `data/`; container memory limits sized to NFR-3. `hikrad-acct`/`hikrad-monitor` entries added commented-out (Phase 2). [FR-49.2, NFR-2 restart posture]
3. `deploy/.env.example` + `scripts/gen-env.sh`: generates `HIKRAD_DB_URL`, `HIKRAD_REDIS_URL`, random `HIKRAD_ENCRYPTION_KEY` (32B base64), `HIKRAD_JWT_SECRET`, `HIKRAD_ENV`. [FR-49.1]
4. `scripts/install.sh` v0: OS check (Ubuntu 22.04/24.04), Docker install if missing, layout `/opt/hikrad/{data,backups,licenses}`, gen-env, compose up. Idempotent: re-run detects existing install and aborts with a message (update flow is Phase 5). [FR-49.1, FR-49.4]
5. `deploy/caddy/Caddyfile`: `/` → panel dist, `/portal` → portal dist, `/api` → hikrad-api:8080; self-signed TLS default with commented Let's Encrypt block. [FR-49.5, NFR-4.4]
6. `backend/internal/platform/`: config loader (env → typed struct), `Settings` service over the `settings` table (typed get/set, in-process cache with invalidation hook) with v1 keys seeded: `locale.timezone=Asia/Baghdad`, `locale.currency=IQD`, plus empty SMTP/Telegram groups; migration runner wiring (golang-migrate, runs on api boot). Migrations `0010_settings.sql`, `0011_managers.sql` exactly per contract C6. [FR-53.1, FR-53.2 subset]
7. `.github/workflows/ci.yml`: jobs — Go vet/build/test (with postgres+redis services), frontend install/lint/build + `i18n:check`, harness smoke (invokes Agent 2's `make -C backend test-harness-smoke` target; skip gracefully if target absent until merge). [NFR-8]
8. `docs/ops/dev-setup.md`: clean-machine → gate steps 1–4 reproduction guide.

Edge cases: compose must boot with only `.env` present (no internet needed post-image-pull — NFR-7); postgres healthcheck must gate api start; settings cache must be concurrency-safe.

## Contracts consumed/exposed
- **Exposes:** C5 topology + env names (everyone), `platform.Settings` interface (`Get[T](key)`, `Set(key, v)`) consumed by D/B/C in later phases; migration runner conventions; `Makefile` targets CI and other agents call.
- **Consumes:** C1 layout; C3 `Deps` shape (platform provides DB/Redis/Settings constructors used by D's main).

## Definition of done
- Gate items 1, 2, 5, 6 pass locally: fresh clone → `make up seed` → healthy stack; CI workflow green on the merge PR.
- Unit tests: settings get/set/cache-invalidation; config loader env parsing; gen-env produces valid unique secrets twice.
- `install.sh` run twice on a clean Ubuntu VM: first run installs, second refuses politely.

## Handoff
Phase 2 agents receive: running stack + env contract, `platform.Settings`, migration tooling with their reserved ranges, CI that will run their tests, and commented-out service stubs for `hikrad-acct`/`hikrad-monitor` to enable.
