# Phase v4-2 — Native install, Docker removal, RouterOS target

> **FRs:** FR-118, FR-119, FR-120, FR-121 · **Migrations:** none · **Depends on:** v4-0 · **Blocks:** v4-3 (release pipeline)
> **Owner sub-PRD:** 01 (platform) · **Decisions committed at kickoff:** 57, 58, 59

## 1. Intent

Docker is the product's largest field liability: install failures, permission problems (the branding-directory `mkdir` failure), volume and data-path confusion, and an update path that has to orchestrate containers. SAMM installs natively with systemd and operators succeed with it. HikRAD moves to the same model.

**The apparent contradiction, resolved (Decision 58):** RouterOS's container feature is the *only* way to run software on a MikroTik router, and it consumes an **OCI image**, not a Docker daemon. Keeping the RouterOS target therefore does not reinstate Docker anywhere a customer touches — one image is built in CI for that one device target. No Compose file ships, no Docker is installed on any Linux server, and the image is never the Linux install path.

## 2. Scope

**In:** the native installer, the systemd unit set, nginx replacing Caddy, Docker artefact deletion, the RouterOS image and its procedure, the updater retarget, the `hikrad` CLI rewrite, and the Compose→native migration path for existing installs.
**Not in:** licensing (v4-3), the signed-bundle format changes beyond dropping images (v4-3), any feature work.

## 3. Work

### 3.1 FR-118 — Native installer
Rewrite `scripts/install.sh`. Supported: Ubuntu 22.04/24.04/26.04 LTS, Debian 12/13; other Linux accepted with a warning (Decision 55 stands).

Provisions, in order, all idempotently:
1. OS detection and dependency install.
2. **PostgreSQL** — cluster, role, database, and the TimescaleDB/partitioning extension; detects an existing local PostgreSQL and uses it or refuses with a stated reason.
3. **FreeRADIUS 3** — config from `deploy/freeradius/` retargeted at native paths, database-backed **dynamic NAS clients** (adding a router must never need a restart), and `freeradius -CX` validation **before** the unit is enabled.
4. **Binaries** to `/opt/hikrad/bin`, data to `/var/lib/hikrad`, config to `/etc/hikrad`, logs via journald.
5. **Directory creation and ownership on both fresh install and upgrade** — branding, uploads, backups, accounting spill. This is the fix for the recurring permission class of bug (anti-pattern A11); it is a gate item, not a side effect.
6. **nginx** (Decision 59) — additive, existing sites untouched; port 80 or a prompted alternative, remembered across re-runs.
7. **systemd units** — `hikrad-api`, `hikrad-radius-worker`, `hikrad-acct`, `hikrad-monitor`, `hikrad-notification`, `hikrad-updaterd`. Each runs as a dedicated non-root user with `ProtectSystem`, `PrivateTmp`, and a `Restart=` policy.
8. WireGuard and `cloudflared` installed but inert until configured.
9. Summary output: URL, default credentials with a change-now warning, config paths.

Contract properties (all gate items): **idempotent** re-run upgrades in place, never overwrites config, never regenerates existing secrets, preserves the chosen port; **non-interactive** via `HIKRAD_DB_PASS`, `HIKRAD_HTTP_PORT`, `HIKRAD_CF_TOKEN`, `HIKRAD_VERBOSE`, `NO_COLOR`; **every prompt before work begins**, never mid-run.

### 3.2 FR-119 — Docker removal
Delete `deploy/compose.yml`, `deploy/docker/` (four Dockerfiles + `healthcheck.go`), `deploy/caddy/`, and `scripts/render-release-compose.sh`. Rework `gen-env.sh` from a Compose `.env` generator into a native `/etc/hikrad/hikrad.env` generator. Rewrite `scripts/hikrad` so every `docker compose` call becomes `systemctl` or a direct binary call — `status`, `logs`, `restart`, `backup`, `restore`, `update`, `uninstall`, `factory-reset`, plus a new `doctor` (health check) and `passwd` (admin password reset), matching SAMM's maintenance CLI.

Rework `build-release-bundle.sh` to package binaries, the frontend build, migrations, FreeRADIUS config, scripts, and the checksum manifest. `verify-bundle.sh`'s signature verification is unchanged — it is the one thing that must keep working byte-for-byte.

### 3.3 FR-120 — RouterOS container target
One all-in-one OCI image (arm64 + amd64) containing all services plus PostgreSQL, published as an importable `.tar` on the release. Documented procedure: enable container mode → **hard reboot** (soft is insufficient) → format and mount an ext4 disk → import and start → point the router's own RADIUS client at loopback.

Document, prominently and twice: **removing the container app destroys the database.** Backup-before-upgrade is mandatory, and `hikrad backup` must be reachable from the on-router install.

Also state the supported ceiling honestly — this target is for small deployments, and the brief must name a subscriber/session limit rather than implying parity with a server.

### 3.4 FR-121 — Updater retarget
`hikrad-updaterd` keeps its shape — unix socket only, token-authenticated, four verbs, no argument reaching a shell — and changes only what it drives: `systemctl` instead of `docker compose`. The `flock` in `cmd_update`, the pre-update backup, the health gate, and the **autonomous rollback that completes with the panel and daemon both dead** are preserved and re-verified, not reimplemented.

### 3.5 Compose → native migration
Existing installs must have a path. `hikrad migrate-to-native`: stops the stack, dumps the database and data directories from the Compose volumes, runs the native installer, restores, verifies, and only then offers to remove the containers. **Rehearsed on a restored pilot backup before any customer runs it** — this is a gate item.

## 4. Frozen contracts

- Paths: binaries `/opt/hikrad/bin`, config `/etc/hikrad`, data `/var/lib/hikrad`, backups `/var/backups/hikrad`.
- Unit names as listed in §3.1; the panel's health page and the updater both address services by these names.
- `hikrad` CLI verb set as listed in §3.2 — the panel's update relay depends on `update` keeping its existing exit-code contract.
- Installer environment variable names as listed in §3.1.

## 5. Acceptance gate

| # | Item | Proof |
|---|---|---|
| 1 | Clean-OS install | Fresh Ubuntu 24.04 VM, one command, stack healthy |
| 2 | **First PPPoE auth under 30 min** | Timed from bare VM to a real MikroTik authenticating a subscriber; elapsed time recorded |
| 3 | Idempotent re-run | Second run changes no config, no secret, no port; `diff` of `/etc/hikrad` before/after is empty |
| 4 | Additive nginx | Pre-existing nginx site still serves after install |
| 5 | Port fallback | Install with 80 occupied; alternative prompted, remembered on re-run |
| 6 | Non-interactive | Full install with only env vars, no TTY |
| 7 | Dirs owned on **upgrade** | Upgrade an install with a wrong-owner branding dir; logo upload works afterwards |
| 8 | FreeRADIUS validated pre-enable | Corrupt the config, confirm install refuses before enabling the unit |
| 9 | Dynamic NAS clients | Add a router via the panel, authenticate through it, with no FreeRADIUS restart |
| 10 | Docker fully gone | `grep -ri docker` over `deploy/ scripts/ .github/` returns only the RouterOS image job |
| 11 | Rollback autonomous | Deliberately broken update rolls back with panel and daemon killed |
| 12 | Compose→native migration | Rehearsed on a restored pilot backup; row counts and money book match before/after |
| 13 | **RouterOS install** | Real 7.23+ device, image imported, hotspot subscriber authenticates. **Hardware leg, declared at kickoff** |
| 14 | Backup reachable on-router | `hikrad backup` produces a downloadable file from the container install |

## 6. Commit plan

1. `feat(install): native systemd installer for Ubuntu/Debian (FR-118)`
2. `feat(install): systemd unit set with per-service users and hardening (FR-118)`
3. `feat(install): nginx replaces Caddy, additive to existing sites (FR-118, Decision 59)`
4. `fix(install): create and chown data directories on upgrade as well as fresh install (FR-118)`
5. `feat(cli): rewrite hikrad CLI onto systemd; add doctor and passwd (FR-119)`
6. `build: package binary release bundles, drop image packaging (FR-119)`
7. `chore(deploy): delete compose.yml, Dockerfiles, Caddy config (FR-119, Decision 57)`
8. `feat(updater): retarget hikrad-updaterd from compose to systemd (FR-121)`
9. `feat(install): RouterOS container image and on-router procedure (FR-120, Decision 58)`
10. `feat(cli): hikrad migrate-to-native for existing Compose installs (FR-118)`
11. `docs(ops): rewrite install.md — native only, three paths (FR-118)`
12. `docs: v4-2 gate result`

## 7. Kickoff prompt

```
Execute phase v4-2 (Native install, Docker removal, RouterOS target) of the HikRAD v4
program, solo and sequentially, start to finish, in this repo.

Read first:
  - docs/v4/00-v4-master-plan.md            (Decisions 57, 58, 59)
  - docs/v4/phases/phase-v4-2-native-install/00-phase.md   (this brief)
  - scripts/install.sh, scripts/hikrad, scripts/quick-install.sh
  - scripts/build-release-bundle.sh, scripts/verify-bundle.sh
  - deploy/compose.yml, deploy/docker/, deploy/caddy/, deploy/freeradius/
  - docs/ops/update.md and the existing hikrad-updaterd implementation

Deliver FR-118, FR-119, FR-120, FR-121.

Understand the one apparent contradiction before you start: Docker is CANCELLED as the
delivery mechanism (Decision 57), and the RouterOS container target is KEPT (Decision 58).
These do not conflict. RouterOS's container feature is the only way to run software on a
MikroTik router and it consumes an OCI image, not a Docker daemon. CI builds exactly one
image for that one device target. No Compose file ships. No Docker is installed on any
Linux server. The image is never the Linux install path.

Priorities in order:
1. The native installer, and its idempotency. Re-running it must upgrade in place without
   overwriting config, regenerating secrets, or changing the chosen port.
2. Directory creation and ownership on the UPGRADE path, not just fresh install. This is
   the recurring bug class (anti-pattern A11 — the branding mkdir permission failure hit
   every existing install because the fix only touched the fresh path). Gate item 7.
3. Preserving what already works: verify-bundle.sh's signature check, the updater's flock,
   the pre-update backup, the health gate, and the autonomous rollback. RETARGET these to
   systemd; do not reimplement them. The rollback must still complete with the panel and
   the daemon both dead (gate item 11).
4. A rehearsed migration path for existing Compose installs (gate item 12). Rehearse on a
   restored pilot backup. No customer runs an unrehearsed data migration.

Gate items 2 (timed 30-minute install), 13 (real RouterOS device), and 12 (restored pilot
backup) need hardware or real data. They are declared here at kickoff per the program rule.
If you cannot run one, say so plainly in the gate result and mark the phase INCOMPLETE.
Do not mark GREEN with hardware legs pending — that is the exact anti-pattern (A1) this
program exists to stop.

Finish with docs/history/gates/v4-2.md.
```
