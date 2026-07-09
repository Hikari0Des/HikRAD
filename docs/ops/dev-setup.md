# Developer setup — clean machine to a running HikRAD stack

This guide reproduces Phase-1 integration gate steps 1–4 on a fresh machine
(gate definition: [docs/phases/phase-1-foundation/00-phase.md](../phases/phase-1-foundation/00-phase.md)).

## 0. Prerequisites

| Tool | Version | Needed for |
|---|---|---|
| Docker Engine + Compose plugin | 24+ / v2 | the whole stack |
| GNU make, bash, openssl | any recent | `make` targets, secret generation |
| Go | 1.22+ | only for running backend tests outside containers |
| Node.js + npm | 20+ | only for frontend work (`frontend/` npm workspaces) |

Linux or macOS assumed; on Windows use WSL2 (the shell scripts are bash) —
and clone the repo into the WSL2 distro's own filesystem (e.g. `~/hikrad`),
**not** a `/mnt/c/...` passthrough of a Windows path. FreeRADIUS refuses to
start ("insecure configuration") if `deploy/freeradius/` is bind-mounted from
a Windows-backed path, because Windows bind mounts commonly present files as
group/world-writable regardless of their real permissions — confirmed while
building this gate; see `deploy/freeradius/README.md`'s last section.

```sh
git clone <repo-url> hikrad && cd hikrad
```

## 1. Gate step 1 — stack up and healthy

```sh
make up
```

What happens: `scripts/gen-env.sh` writes `deploy/.env` (fresh secrets,
`HIKRAD_ENV=dev` — required for the dev login stub), then
`docker compose up -d --build` starts `postgres`, `redis`, `hikrad-api`,
`freeradius`, `caddy`. Postgres health gates `hikrad-api` start; `hikrad-api`
runs all migrations in `backend/migrations/` on boot before serving.

Verify — every service `healthy`:

```sh
docker compose --env-file deploy/.env -f deploy/compose.yml ps
```

State lives in `deploy/data/` (gitignored). `make down` stops the stack and
keeps it; delete `deploy/data/` for a truly clean slate.

## 2. Gate step 2 — seed demo data

```sh
make seed
```

Loads the Phase-1 demo set (Agent D's seeder): manager `admin`/`admin`,
subscriber `testuser`/`testpass`, one profile. Idempotent — safe to re-run.

## 3. Gate step 3 — RADIUS round-trip

```sh
make -C backend test-harness-smoke
```

Agent B's packet harness sends a PPPoE-style Access-Request for
`testuser`/`testpass` to `freeradius` (UDP 1812) and expects Access-Accept
carrying `Mikrotik-Rate-Limit = "10M/10M"`; a wrong password must yield
Access-Reject. Manual alternative with radclient:

```sh
echo 'User-Name = "testuser", User-Password = "testpass"' | \
  radclient -x 127.0.0.1:1812 auth <radius-secret-from-deploy/freeradius>
```

## 4. Gate step 4 — panel and portal

The Caddy container serves prebuilt frontend dists, so build them once:

```sh
cd frontend && npm ci && npm run build --workspaces && cd ..
```

- Panel: <https://localhost/> — accept the self-signed certificate warning
  (offline default; see `deploy/caddy/Caddyfile` to switch to Let's Encrypt),
  log in as `admin`/`admin` (dev stub, contract C7) → empty dashboard shell.
- Portal: <https://localhost/portal> — must render in all three locales
  (en/ar/ku) with correct RTL flip for ar/ku.

## 5. Tests & CI parity

```sh
make test      # backend go test + scripts/gen-env.test.sh
make lint      # go vet + frontend lint
cd frontend && npm run i18n:check   # hardcoded-string check (CI-fatal)
```

CI (`.github/workflows/ci.yml`) runs the same four jobs: Go
vet/build/test (-race, with postgres+redis services), frontend
lint/build/i18n:check, ops-script self-tests, and the harness smoke test.
Each job skips itself with a notice while its inputs haven't merged yet.

## Troubleshooting

- **`hikrad-api` unhealthy / restarting** — `docker compose … logs hikrad-api`.
  Most common: stale `deploy/.env` missing a variable (regenerate:
  `./scripts/gen-env.sh --force deploy/.env`, then `make up`) or a failed
  migration (fix the SQL; migrations are forward-only, so also
  `migrate force` or reset `deploy/data/postgres` in dev).
- **Compose warns `POSTGRES_PASSWORD` unset** — `deploy/.env` missing; run
  `make env`.
- **Panel shows Caddy 404/empty page** — frontend dists not built (step 4).
- **Port clash on 80/443/1812** — stop the conflicting service; the RADIUS
  ports are host-published by design (NAS devices must reach them).
- **No internet on the target box** — expected to work: after images are
  pulled/loaded once, nothing in daily operation needs the internet (NFR-7).

## Production install (reference)

Not needed for development: `sudo ./scripts/install.sh` on Ubuntu 22.04/24.04
creates `/opt/hikrad/{data,backups,licenses}`, generates `/opt/hikrad/.env`
with `HIKRAD_ENV=prod`, installs the `hikrad` CLI, and starts the stack.
Re-running against an existing install aborts without touching data (FR-49.4).
