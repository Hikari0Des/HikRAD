# Developer setup — clean machine to a running HikRAD stack

**v4-2 (Decision 57): no Docker.** The dev inner loop runs the Go services
directly (`go run`) against a locally installed PostgreSQL 16 + TimescaleDB
and Redis — the same services production uses, just not under systemd.
This is lighter and faster than the old Compose loop, not a downgrade.

## 0. Prerequisites

| Tool | Version | Needed for |
|---|---|---|
| Go | 1.22+ | backend services, tests |
| Node.js + npm | 20+ | frontend work (`frontend/` npm workspaces) |
| PostgreSQL | 16 | app database |
| TimescaleDB | 2.x (postgresql-16 build) | usage-point hypertables |
| Redis | 7+ | accounting queue, live-session cache |
| FreeRADIUS | 3.2.x | only if you're testing the RADIUS path itself |

### Getting Postgres/Redis/FreeRADIUS running

**Linux (Ubuntu/Debian):** the exact packages/repos `scripts/install.sh`
itself adds — reuse its logic rather than guessing:

```sh
sudo apt-get update
sudo apt-get install -y postgresql postgresql-contrib redis-server freeradius freeradius-utils
# TimescaleDB needs its own apt repo — see scripts/install.sh's "PostgreSQL + TimescaleDB" step
# for the exact commands (adds packagecloud.io/timescale + apt.postgresql.org, idempotently).
sudo -u postgres psql -c "CREATE ROLE hikrad LOGIN PASSWORD 'devpassword';"
sudo -u postgres psql -c "CREATE DATABASE hikrad OWNER hikrad;"
sudo -u postgres psql -d hikrad -c "CREATE EXTENSION IF NOT EXISTS timescaledb;"
```

**macOS:** `brew install postgresql@16 timescaledb redis freeradius`, then
the same `CREATE ROLE`/`CREATE DATABASE` as above.

**Windows:** use **WSL2** (a real Linux VM, not emulation) — the shell
scripts are bash and FreeRADIUS specifically refuses to start against a
Windows-backed bind mount's permissive file permissions. Install a distro
(`wsl --install -d Debian` or `Ubuntu`), **enable systemd** (add
`[boot]\nsystemd=true` to `/etc/wsl.conf` inside the distro, then
`wsl --shutdown` from PowerShell and reopen it), then follow the Linux
steps above inside WSL2. Clone the repo into the distro's **own**
filesystem (e.g. `~/hikrad`), **not** a `/mnt/c/...` passthrough of a
Windows path — same reasoning as the FreeRADIUS permission note above.

With systemd enabled, WSL2 can *also* run the real production installer
end to end (`sudo ./scripts/install.sh`) for a genuine test of the native
install path on a Windows machine — Ubuntu 22.04/24.04/26.04 and Debian
12/13 are both directly supported. That's a heavier, more realistic check
than the dev loop below; use it when you want to prove the actual
installer works, not just iterate on application code.

```sh
git clone <repo-url> ~/hikrad && cd ~/hikrad
```

## 1. Generate a dev env file

```sh
make dev-env
```

Writes `deploy/hikrad.env` with fresh secrets (`HIKRAD_ENV=dev` — required
for the dev login stub), pointed at `127.0.0.1:5432`/`127.0.0.1:6379`.
Refuses to overwrite an existing file — delete it first if you need fresh
secrets (see "Resetting" below; you'll also need to reset the database
since the encryption key changes).

## 2. Start the backend services

```sh
make dev-up
```

Runs `hikrad-api`, `hikrad-acct`, `hikrad-monitor` in the foreground via
`go run` (first start is slower — it compiles each package); migrations
run automatically when `hikrad-api` boots. **Ctrl+C stops all three.**
Logs are prefixed `[api]`/`[acct]`/`[monitor]` on the same terminal.

## 3. Seed demo data

```sh
make seed
```

Loads the demo set: manager `admin`/`admin`, subscriber `testuser`/
`testpass`, one profile, one NAS at `10.0.0.1`. Idempotent — safe to
re-run (with `dev-up` still running in another terminal, or standalone).

## 4. RADIUS round-trip (optional — only if you're testing the RADIUS path)

FreeRADIUS isn't started by `make dev-up` (most panel/API/frontend work
never needs it). Point it at the repo's config directly:

```sh
sudo freeradius -X -d deploy/freeradius   # -X: full debug output, foreground
```

(`sudo` because FreeRADIUS needs to read its own config; on a real install
`scripts/install.sh` retargets `raddbdir`/`logdir`/`run_dir` into
`/etc/hikrad/freeradius` first — running straight from the repo checkout
like this is a dev-only shortcut and logs to your terminal instead.)

In another terminal:

```sh
cd backend
go run ./test/harness -addr 127.0.0.1:1812 -secret testing123
```

Sends a PPPoE-style Access-Request for `testuser`/`testpass` and expects
Access-Accept carrying `Mikrotik-Rate-Limit = "10M/10M"`; a wrong password
must yield Access-Reject. Or manually with `radclient`:

```sh
echo 'User-Name = "testuser", User-Password = "testpass"' | radclient -x 127.0.0.1:1812 auth testing123
```

`make -C backend test-harness-smoke` runs this whole round-trip
end-to-end (starts hikrad-api, seeds, starts FreeRADIUS, runs the harness,
tears everything down) — the same target CI's `harness-smoke` job runs.

## 5. Panel and portal

```sh
cd frontend/panel && npm ci && npm run dev    # http://localhost:5173, proxies /api to :8080
cd frontend/portal && npm ci && npm run dev   # separate terminal, separate port
```

Vite's dev server hot-reloads on save — no build step needed while
iterating. Log in as `admin`/`admin` (dev stub, only active when
`HIKRAD_ENV=dev`).

## 6. Tests & CI parity

```sh
make test      # backend go test + scripts/gen-env.test.sh
make lint      # go vet + frontend lint
cd frontend && npm run i18n:check   # hardcoded-string check (CI-fatal)
```

CI (`.github/workflows/ci.yml`) runs the same jobs: Go vet/build/test
(-race, with Postgres+Redis GitHub Actions service containers), frontend
lint/build/i18n:check, ops-script self-tests (native `install.sh`
idempotency, sandboxed), and the harness smoke test (native FreeRADIUS,
no Docker).

## Resetting

```sh
make dev-down          # drops and recreates the dev database (empty; re-run migrations via dev-up/seed)
rm deploy/hikrad.env    # also want fresh secrets? delete this too, then 'make dev-env' again
```

`make dev-down` is `scripts/dev-run.sh --reset-db-only` — it terminates
connections, drops, and recreates the `hikrad` database (re-creating the
TimescaleDB extension), then exits. It does **not** touch
`deploy/hikrad.env` — your encryption key/JWT secret stay the same, so
existing browser sessions/tokens remain structurally valid even though the
data behind them is gone. Nothing under `frontend/` is touched by either
command — clear `node_modules`/`dist` yourself if you need a truly clean
frontend rebuild.

To reset **everything** including secrets: `rm deploy/hikrad.env`, drop the
database manually (`sudo -u postgres psql -c 'DROP DATABASE hikrad;'`,
`CREATE DATABASE` + `CREATE EXTENSION` again), then `make dev-env`.

## Troubleshooting

- **`hikrad-api` won't start / connection refused on :5432 or :6379** —
  confirm Postgres/Redis are actually running (`systemctl status
  postgresql redis-server` on Linux/WSL2, `brew services list` on macOS)
  and that `deploy/hikrad.env`'s `HIKRAD_DB_URL`/`HIKRAD_REDIS_URL` match
  how you set them up (default: role `hikrad`, database `hikrad`, both on
  `127.0.0.1`).
- **Migration failure on boot** — migrations are forward-only; fix the SQL
  or `make dev-down` for a clean slate in dev (never do this in production).
- **Panel shows a blank/error page** — confirm `npm run dev` is still
  running in that terminal and `hikrad-api` is reachable at `:8080`.
- **Port clash on 8080/1812** — stop whatever else is using it; RADIUS
  ports need to be free for a real NAS/harness to reach them.
- **No internet on the target box (production)** — expected to work: after
  the release bundle is downloaded once, nothing in daily operation needs
  the internet (NFR-7).

## Production install (reference)

Not needed for development: `sudo ./scripts/install.sh` on a supported OS
creates `/opt/hikrad/bin`, `/etc/hikrad`, `/var/lib/hikrad`,
`/var/backups/hikrad`, generates `/etc/hikrad/hikrad.env` with
`HIKRAD_ENV=prod`, installs the `hikrad` CLI, and starts the stack under
systemd. Re-running against an existing install upgrades it in place,
never touching secrets or the chosen port. Full walkthrough:
[install-guide.md](install-guide.md).
