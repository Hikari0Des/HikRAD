# HikRAD

RADIUS AAA + billing platform for Iraqi ISPs — a self-hosted Snono SAS4 alternative sold as a
one-time license and installed on-premise with Docker. Go backend, FreeRADIUS 3.2,
PostgreSQL 16 + TimescaleDB, Redis, React 18 panel + subscriber portal (en/ar/ku, true RTL),
all behind Caddy on a single server (4 vCPU / 8 GB / 200 GB tier).

Core product claim: **never lose an Accounting-Request** — a lossless accounting pipeline with
durable ingest, idempotent replay, and audited counters, plus real-time session visibility.

## Repo layout

| Path | What it is |
|---|---|
| `deploy/` | Docker Compose stack, Caddy config, `.env.example` (`deploy/freeradius/` holds the FreeRADIUS config) |
| `scripts/` | `install.sh` (production installer), `gen-env.sh` (secret generation), `hikrad` (ops CLI wrapper) |
| `backend/` | Single Go module `github.com/hikrad/hikrad` — binaries `hikrad-api`, `hikrad-acct`, `hikrad-monitor`; SQL migrations in `backend/migrations/` |
| `frontend/` | npm workspaces: `panel` (admin), `portal` (subscriber PWA), `shared` (`@hikrad/shared` i18n) |
| `docs/` | PRD (source of truth), domain sub-PRDs, phased execution plan, ops guides |

## Quickstart (development)

Prerequisites: Docker Engine + Compose plugin, GNU make, bash. Go 1.22+ and Node 20+ only if
you work on the code outside containers.

```sh
git clone <repo> hikrad && cd hikrad
make up      # generates deploy/.env on first run, then docker compose up -d --build
make seed    # demo data: admin/admin manager, testuser subscriber, one profile
make test    # backend unit tests + script self-tests
make down    # stop the stack
```

The panel is served at `https://localhost/` (self-signed cert by default), the subscriber
portal at `https://localhost/portal`, the API under `https://localhost/api/v1`. RADIUS auth,
accounting, and CoA listen on UDP 1812/1813/3799.

Full walkthrough (including the RADIUS packet harness): [docs/ops/dev-setup.md](docs/ops/dev-setup.md).

## Production install

On a clean Ubuntu 22.04/24.04 server:

```sh
sudo ./scripts/install.sh
```

Installs Docker if missing, creates `/opt/hikrad/{data,backups,licenses}`, generates secrets
into `/opt/hikrad/.env`, and starts the stack. Re-running on an installed server aborts safely
(the update flow ships in a later phase). Daily operation requires no internet access.

## Documentation

- [docs/PRD.md](docs/PRD.md) — master PRD (order of truth #1)
- [docs/prd/](docs/prd/00-index.md) — domain sub-PRDs
- [docs/phases/](docs/phases/00-team.md) — phased multi-agent execution plan
- [docs/ops/dev-setup.md](docs/ops/dev-setup.md) — developer setup guide

## License

Commercial. © HikRAD. Not open source; see the master PRD for licensing model (FR-50).
