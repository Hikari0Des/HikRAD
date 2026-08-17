# Install guide — clean server to a working PPPoE user in under 30 minutes

This is the **M4 document**: a network technician (persona Ali — MikroTik
expert, Linux basics, no HikRAD experience) should be able to follow this
guide alone, with no other context, and reach a real PPPoE Access-Accept in
under 30 minutes on a clean server. If any step here is unclear or wrong,
that is a bug in this document, not in you — file it.

**v4-2 (Decision 57): the install is native — systemd units, no Docker,
no Compose file, nothing to build on the customer's server unless you're
running from source.** A separate, single-image RouterOS container target
exists for MikroTik hardware itself (Decision 58) — see
[deploy/routeros/README.md](../../deploy/routeros/README.md); it is not
what this document covers.

## 0. Before you start

You need:
- A fresh Ubuntu 22.04/24.04/26.04 LTS or Debian 12/13 server (VM or bare
  metal), reachable by SSH, with `sudo`. Other modern Linux distributions
  are accepted with a warning instead of blocking (Decision 55).
- Recommended hardware: 4 vCPU / 8 GB RAM / 200 GB SSD (NFR-3). Smaller
  works for a lab/pilot — the installer warns, never blocks.
- A MikroTik router (or any NAS you can point at this server) for the final
  PPPoE test. A VM/CHR works fine for a rehearsal.
- Optional: a public domain name pointed at this server, if you want a real
  (Let's Encrypt, via certbot) TLS certificate instead of the default
  self-signed one.
- Optional: a license file from HikRAD if you already have one. If not,
  the wizard shows this server's fingerprint so you can request one — the
  system runs unrestricted while unlicensed.

## 1. Get the release and run the installer

**Recommended — one command**, no source access needed:

```sh
curl -fsSL https://raw.githubusercontent.com/Hikari0Des/hikrad-releases/main/install.sh | sudo bash
```

This downloads the latest signed `hikrad-vX.Y.Z.tar` release bundle and
hands off to its own `install.sh --bundle`, which **re-verifies the whole
bundle's checksums and Ed25519 signature against a public key embedded in
the script before touching anything** — a corrupted download or a tampered
file anywhere in it is refused with no partial effect.

Extra flags pass straight through, e.g. for a real TLS certificate (needs
the domain's DNS pointed here first):

```sh
curl -fsSL https://raw.githubusercontent.com/Hikari0Des/hikrad-releases/main/install.sh | sudo bash -s -- --domain panel.your-isp.example
```

**Manual bundle install** (already have the tar):

```sh
sudo ./scripts/install.sh --bundle hikrad-vX.Y.Z.tar
```

**Source-mode (internal/dev only)** — requires access to this private repo
and a Go/Node toolchain on the target server; builds binaries locally
instead of loading a signed bundle:

```sh
git clone <repo-url> hikrad && cd hikrad
sudo ./scripts/install.sh
```

Non-interactively (no TTY, e.g. scripted): set `HIKRAD_DB_PASS`,
`HIKRAD_HTTP_PORT`, `HIKRAD_CF_TOKEN` as needed before running either
command — every prompt the installer would otherwise ask happens before
any change is made, never mid-run.

### What it does

1. Checks the OS and hardware tier (warns, never blocks, on a small box).
2. Provisions **PostgreSQL 16 + TimescaleDB** (adds the upstream apt repos
   if missing, creates the `hikrad` role/database) — detects an existing
   local PostgreSQL and uses it, or refuses with a stated reason.
3. Provisions **FreeRADIUS 3** at `/etc/hikrad/freeradius`, with
   **database-backed dynamic NAS clients** — adding a router in the panel
   authenticates through it immediately, with **no FreeRADIUS restart**
   (see `deploy/freeradius/README.md`'s "DB-driven clients" section for the
   mechanism). Validates the config (`freeradius -CX`) **before** enabling
   the unit — a corrupt config is refused, not half-started.
4. Installs binaries to `/opt/hikrad/bin`, data to `/var/lib/hikrad`,
   config to `/etc/hikrad`, backups to `/var/backups/hikrad`. Logs go to
   journald (`journalctl -u hikrad-api`, etc.) — no log files to manage.
5. Creates and owns every data directory the services need — **on both a
   fresh install and every re-run/upgrade**, not just the first time (the
   recurring class of bug this fixes: a directory-ownership fix that only
   touched the fresh-install path used to break every *existing* install
   the moment they upgraded).
6. Installs **nginx** additively — your existing nginx sites, if any,
   keep serving. Port 80/443 by default; if 80 is already taken, you're
   prompted for an alternative (or set `HIKRAD_HTTP_PORT`), and that choice
   is remembered on every future re-run.
7. Installs and enables the six `hikrad-*` systemd units (`hikrad-api`,
   `hikrad-radius-worker`, `hikrad-acct`, `hikrad-monitor`,
   `hikrad-notification` — not started until its binary ships in a later
   phase, `hikrad-updaterd`), each running as a dedicated non-root user
   with `ProtectSystem=strict`/`PrivateTmp`/`Restart=on-failure`.
8. Installs WireGuard and `cloudflared` (inert until configured — Settings
   > Remote Access, or `hikrad tunnel enable`).
9. Prints a summary: panel URL, config/data paths, and the backup
   passphrase (**shown once — write it down**, see
   [backup-restore.md](backup-restore.md)).

**Re-running `install.sh` against an existing install upgrades it in
place**: secrets in `/etc/hikrad/hikrad.env` are never regenerated, the
chosen port never changes, and every directory ownership fix above is
re-applied. This is also exactly what `hikrad update` does under the hood.

**Verify:**
```sh
hikrad status
hikrad doctor    # every unit active, Postgres reachable, FreeRADIUS/nginx config valid, hikrad-api answering
```

## 2. First-run wizard

Open `https://<this-server>/` in a browser (accept the self-signed
certificate warning if you didn't use `--domain`). With no admin account yet,
the panel serves the first-run wizard instead of a login screen.

1. **License.** The wizard shows this server's fingerprint (also available
   at `GET /api/v1/setup/license`) as copyable text. If you have a license
   file already, upload it here. If not, skip for now.
2. **Admin account.** Choose a username and a password (≥ 8 characters).
3. **Branding.** ISP name, logo URL, and brand colors (optional).
4. **First NAS.** Add your MikroTik router (see
   [docs/prd/02-radius-nas-aaa.md](../prd/02-radius-nas-aaa.md)).
5. **First profile.** Create a subscriber profile (rate limit, session/quota
   behavior) — see [docs/prd/04-subscribers-profiles.md](../prd/04-subscribers-profiles.md).

## 3. Prove it: a real PPPoE Access-Accept

Add a test subscriber (panel: Subscribers > New, using the profile from step
4 above), then either dial in from a real PPPoE client against your NAS, or
simulate the NAS side directly with the repo's packet harness:

```sh
cd backend
go run ./test/harness -addr <this-server-ip>:1812 -secret <the NAS's RADIUS secret>
```

A `5-case smoke suite (PAP/CHAP accept+reject)` run with your test
subscriber's credentials passing is your M4 finish line. Note the wall-clock
time from step 1's install command to this passing test — that's the number
this document is held to (< 30 minutes).

## Appendix: wizard by API

```sh
API=https://<this-server>/api/v1
curl -s $API/setup/status
FP=$(curl -s $API/setup/license | grep -o '"fingerprint":"[^"]*"' | cut -d'"' -f4)
curl -s -X POST $API/setup/license -H 'Content-Type: application/json' -d @license.json   # optional
curl -s -X POST $API/setup/admin -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"choose-a-real-password"}'
curl -s -X POST $API/setup/branding -H 'Content-Type: application/json' -d '{"name":"Your ISP"}'
TOKEN=$(curl -s -X POST $API/auth/login -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"choose-a-real-password"}' \
  | grep -o '"access_token":"[^"]*"' | cut -d'"' -f4)
```

## Firewall / ports the stack uses

**80/443 tcp** (panel/portal via nginx), **1812/1813 udp** (RADIUS
auth/accounting), **3799 udp** (CoA), and **5678 udp** (MikroTik MNDP
neighbor discovery — broadcast discovery only works when this server
shares the routers' L2 segment; the panel's IP-range scan works
regardless).

## Factory reset (wipe all data, keep the install)

```sh
sudo hikrad factory-reset              # interactive: type 'factory reset' to confirm
sudo hikrad factory-reset --yes        # non-interactive
sudo hikrad factory-reset --no-backup  # skip the safety backup (explicit only)
```

Erases **all data** and boots a fresh, empty system on the same server: the
database is dropped and recreated, data directories are recreated with
correct ownership, the generated FreeRADIUS client list is emptied. A
safety backup is taken first unless `--no-backup`. Afterwards the panel
runs the first-run wizard again.

## Uninstalling

```sh
sudo hikrad uninstall              # interactive: type 'uninstall hikrad' to confirm
sudo hikrad uninstall --keep-data  # remove the app but keep the data directory
sudo hikrad uninstall --purge      # remove EVERYTHING including backups + hikrad.env
```

Takes a final backup first (unless `--purge`), stops and disables every
`hikrad-*` unit, removes the nginx site and the CLI wrapper. Backups and
`hikrad.env` are kept by default. PostgreSQL/Redis/nginx/FreeRADIUS
packages are never removed — they may serve other workloads.

## Migrating an existing Docker Compose install

`hikrad migrate-to-native` (or `scripts/migrate-to-native.sh` before the
CLI is installed) — see [update.md](update.md).

See also: [admin-guide.md](admin-guide.md), [pilot-checklist.md](pilot-checklist.md),
[backup-restore.md](backup-restore.md), [update.md](update.md),
[known-issues.md](known-issues.md).
