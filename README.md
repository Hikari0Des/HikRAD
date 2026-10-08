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
| `deploy/systemd/` | The seven `hikrad-*` unit files installed to `/etc/systemd/system/` |
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

Full guide: [docs/ops/install-guide.md](docs/ops/install-guide.md). In short:

**Customer server (offline, from a release):** copy `hikrad-vX.Y.Z.tar` and `install.sh`
into one folder on the server, then

```sh
sudo bash install.sh
```

**Customer server (online, once releases are published):**

```sh
curl -fsSL https://raw.githubusercontent.com/Hikari0Des/hikrad-releases/main/install.sh | sudo bash
```

**Your own server (from this private repo; Go and Node are installed automatically):**

```sh
git clone https://github.com/Hikari0Des/HikRAD.git hikrad && cd hikrad
sudo ./scripts/install.sh
```

Supported: Ubuntu 22.04/24.04, Debian 12/13 (others with a warning). The installer sets up
PostgreSQL + TimescaleDB, Redis, FreeRADIUS, nginx and the `hikrad-*` systemd services,
prints the panel address and a backup passphrase (write it down), and is safe to re-run
(it never regenerates secrets). Afterwards: open the panel, finish the first-run wizard,
and check health with `sudo hikrad doctor`.

**Making a release for customers:** `make release VERSION=v1.2.3` builds the two files into
`dist/`; pushing a `v1.2.3` tag has GitHub build and publish them. See
[docs/ops/release-checklist.md](docs/ops/release-checklist.md).
**Updating:** [docs/ops/update.md](docs/ops/update.md).
**Migrating an old Docker Compose install:** `hikrad migrate-to-native`, see the same page.

## Documentation

- [docs/PRD.md](docs/PRD.md) — master PRD (order of truth #1)
- [docs/prd/](docs/prd/00-index.md) — domain sub-PRDs
- [docs/phases/](docs/phases/00-team.md) — phased multi-agent execution plan
- [docs/ops/dev-setup.md](docs/ops/dev-setup.md) — developer setup guide

## License

Commercial. © HikRAD. Not open source; see the master PRD for licensing model (FR-50).
