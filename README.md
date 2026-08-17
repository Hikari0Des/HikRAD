# HikRAD

RADIUS AAA + billing platform for Iraqi ISPs — a self-hosted Snono SAS4 alternative sold as a
one-time license and installed on-premise natively with systemd (Docker is not part of the
delivery mechanism — v4-2, Decision 57). Go backend, FreeRADIUS 3.2, PostgreSQL 16 +
TimescaleDB, Redis, React 18 panel + subscriber portal (en/ar/ku, true RTL), all behind nginx
on a single server (4 vCPU / 8 GB / 200 GB tier). A separate, single-image RouterOS container
target (Decision 58) exists only for MikroTik's own container feature — see
[deploy/routeros/](deploy/routeros/README.md) — and is never the Linux install path.

Core product claim: **never lose an Accounting-Request** — a lossless accounting pipeline with
durable ingest, idempotent replay, and audited counters, plus real-time session visibility.

## Repo layout

| Path | What it is |
|---|---|
| `deploy/freeradius/` | FreeRADIUS config, retargeted at native paths by `install.sh` |
| `deploy/systemd/` | The six `hikrad-*` unit files installed to `/etc/systemd/system/` |
| `deploy/nginx/` | The nginx site template (additive to any existing nginx config) |
| `deploy/routeros/` | The single RouterOS-container OCI image build (Decision 58) |
| `scripts/` | `install.sh` (native installer), `gen-env.sh` (secret generation), `hikrad` (ops CLI), `migrate-to-native.sh` |
| `backend/` | Single Go module `github.com/hikrad/hikrad` — binaries `hikrad-api`, `hikrad-acct`, `hikrad-monitor`, `hikrad-updaterd`; SQL migrations in `backend/migrations/` |
| `frontend/` | npm workspaces: `panel` (admin), `portal` (subscriber PWA), `shared` (`@hikrad/shared` i18n) |
| `docs/` | PRD (source of truth), domain sub-PRDs, phased execution plan, ops guides |

## Quickstart (development)

Prerequisites: Go 1.22+, Node 20+, a local PostgreSQL 16 + TimescaleDB and Redis (natively
installed, or via WSL2 on Windows). No Docker involved.

```sh
git clone <repo> hikrad && cd hikrad
make dev-env     # generates deploy/hikrad.env against your local Postgres/Redis
make dev-up      # runs hikrad-api/hikrad-acct/hikrad-monitor via `go run`, foreground
make seed        # demo data: admin/admin manager, testuser subscriber, one profile
make test        # backend unit tests + script self-tests
```

Full walkthrough (installing Postgres/Redis/FreeRADIUS locally, the RADIUS packet harness,
and how to reset a dev environment): [docs/ops/dev-setup.md](docs/ops/dev-setup.md).

## Production install

**Recommended — one command**, no source access needed (fetches the latest signed release bundle from the public, source-free mirror repo — see [docs/ops/release-checklist.md](docs/ops/release-checklist.md) and PRD Decision 56):

```sh
curl -fsSL https://raw.githubusercontent.com/Hikari0Des/hikrad-releases/main/install.sh | sudo bash
```

Extra flags (`--domain`, `--no-start`, …) pass straight through, e.g.:

```sh
curl -fsSL https://raw.githubusercontent.com/Hikari0Des/hikrad-releases/main/install.sh | sudo bash -s -- --domain panel.example.isp
```

**Source-mode (internal/dev only)** — requires access to this (private) repo and a Go/Node
toolchain on the target server; builds binaries locally instead of loading a signed bundle.
Ubuntu 22.04/24.04/26.04 LTS and Debian 12/13 are the tested tier, most other modern Linux
distributions work too with a warning:

```sh
sudo ./scripts/install.sh
```

Either way: provisions PostgreSQL + TimescaleDB, FreeRADIUS, nginx, and the six `hikrad-*`
systemd units; creates `/opt/hikrad/bin`, `/etc/hikrad`, `/var/lib/hikrad`,
`/var/backups/hikrad`; generates secrets into `/etc/hikrad/hikrad.env`. Re-running upgrades an
existing install in place — it never overwrites config, regenerates secrets, or changes the
chosen port. Daily operation requires no internet access.

**Migrating an existing Docker Compose install?** See `hikrad migrate-to-native` in
[docs/ops/update.md](docs/ops/update.md).

## Documentation

- [docs/PRD.md](docs/PRD.md) — master PRD (order of truth #1)
- [docs/prd/](docs/prd/00-index.md) — domain sub-PRDs
- [docs/phases/](docs/phases/00-team.md) — phased multi-agent execution plan
- [docs/ops/dev-setup.md](docs/ops/dev-setup.md) — developer setup guide

## License

Commercial. © HikRAD. Not open source; see the master PRD for licensing model (FR-50).
