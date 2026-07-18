# Install guide — clean Ubuntu server to a working PPPoE user in under 30 minutes

This is the **M4 document**: a network technician (persona Ali — MikroTik
expert, Linux basics, no HikRAD experience) should be able to follow this
guide alone, with no other context, and reach a real PPPoE Access-Accept in
under 30 minutes on a clean Ubuntu 22.04/24.04 server. If any step here is
unclear or wrong, that is a bug in this document, not in you — file it.

## 0. Before you start

You need:
- A fresh Ubuntu 22.04 or 24.04 LTS server (VM or bare metal), reachable by
  SSH, with `sudo`.
- Recommended hardware: 4 vCPU / 8 GB RAM / 200 GB SSD (NFR-3). Smaller
  works for a lab/pilot — the installer warns, never blocks.
- A MikroTik router (or any NAS you can point at this server) for the final
  PPPoE test. A VM/CHR works fine for a rehearsal.
- Optional: a public domain name pointed at this server, if you want a real
  (Let's Encrypt) TLS certificate instead of the default self-signed one.
- Optional: a license file from HikRAD if you already have one. If not,
  the wizard shows this server's fingerprint so you can request one — the
  system runs unrestricted while unlicensed, so you don't need it in hand to
  start (sub-PRD 01 §2, FR-50; see step 2 below and
  [admin-guide.md](admin-guide.md#license) for the re-issue flow).

## 1. Get the release and run the installer

**Production (v2 phase 5, FR-81): install from a signed bundle — no source
tree, no Go/Node toolchain, works fully offline.** Vendor-delivered
`hikrad-vX.Y.Z.tar` (see [release-checklist.md](release-checklist.md) for how
it's cut). Extract it somewhere on the server (it carries its own `install.sh`
under `scripts/`), then run that copy pointed back at the tar itself — the
installer re-verifies the whole bundle's checksums and Ed25519 signature
against a public key embedded in the script **before touching anything**,
so a corrupted download or a tampered file anywhere in it is refused with no
partial effect:

```sh
tar -xf hikrad-vX.Y.Z.tar -C hikrad-vX.Y.Z && cd hikrad-vX.Y.Z
sudo ./scripts/install.sh --bundle ../hikrad-vX.Y.Z.tar
# or, for a real TLS certificate (needs the domain's DNS pointed here first):
sudo ./scripts/install.sh --bundle ../hikrad-vX.Y.Z.tar --domain panel.your-isp.example
```

A bit-flipped or otherwise tampered bundle produces a clear refusal (`bundle
verification failed`) and changes nothing — try the download again rather
than proceeding.

**Development only (unchanged, needs a full checkout + Go/Node/Docker
toolchain):**

```sh
git clone <repo-url> hikrad && cd hikrad
sudo ./scripts/install.sh
```

Both modes do the same things (FR-49.1–49.5), differing only in step 6:
1. Checks the OS and hardware tier (warns, never blocks, on a small box).
2. Installs Docker Engine + the Compose plugin if not already present.
3. Creates `/opt/hikrad/{data,backups,licenses}` and generates fresh secrets
   into `/opt/hikrad/.env` — database password, encryption key, JWT signing
   key, and a **backup passphrase**. `--bundle` additionally verifies and
   stages the release into `/opt/hikrad/release/`.
4. Installs the `hikrad` CLI to `/usr/local/bin/hikrad`, a nightly backup
   cron entry, and — best-effort, never blocks the install if it can't —
   `hikrad-updaterd` (a systemd service) so Settings > System's **Update
   now** button works from the panel (v2 phase 7, see
   [update.md](update.md)); the guided `hikrad update` command works either
   way.
5. Configures Caddy for Let's Encrypt if you passed `--domain`; otherwise
   leaves the self-signed default in place.
6. Starts every service: `postgres`, `redis`, `hikrad-api`, `hikrad-acct`,
   `hikrad-monitor`, `freeradius`, `caddy` — **loaded from the bundle's
   images** with `--bundle` (no build step, no internet needed once you have
   the tar), or **built from source** in dev mode.

Bundle mode takes well under a minute once Docker has the images loaded
(nothing to build); source mode takes 3-8 minutes depending on connection
speed. Either way, when it finishes it prints an **install summary with your
backup passphrase — copy it somewhere safe now.** It is not shown again, and
losing both this copy and `/opt/hikrad/.env` makes existing backups
permanently unrecoverable by design (see [backup-restore.md](backup-restore.md)).

Re-running `install.sh` against an existing install never touches `data/` —
it offers an update/repair menu instead (FR-49.4). Which mode an install
started in is recorded in `/opt/hikrad/install.meta` as `HIKRAD_DELIVERY_MODE`
(`bundle` or `source`); `hikrad update` reads it too.

**Verify:**
```sh
hikrad status
```
All services should show `healthy` within about a minute of the install
finishing (Caddy/hikrad-api/hikrad-acct have short startup healthchecks;
freeradius reports up once its process is alive).

## 2. First-run wizard

Open `https://<this-server>/` in a browser (accept the self-signed
certificate warning if you didn't use `--domain`). With no admin account yet,
the panel serves the first-run wizard instead of a login screen.

1. **License.** The wizard shows this server's fingerprint (also available
   at `GET /api/v1/setup/license`) as copyable text. If you have a license
   file already, upload it here. If not, skip for now — the wizard and the
   rest of setup work unrestricted (grace/read-only behavior only applies to
   an *installed* license whose fingerprint later stops matching, per
   FR-50.3; a fresh install with no license at all is simply unlicensed, not
   grace-limited).
2. **Admin account.** Choose a username and a password (≥ 8 characters).
   This creates the one administrator account the wizard is allowed to
   create — every setup endpoint closes immediately afterward.
3. **Branding.** ISP name, logo URL, and brand colors (all optional — skip
   and set these later under Settings > Branding).
4. **First NAS.** Add your MikroTik router (see
   [docs/prd/02-radius-nas-aaa.md](../prd/02-radius-nas-aaa.md) for the NAS
   wizard content and RouterOS snippet this step generates).
5. **First profile.** Create a subscriber profile (rate limit, session/quota
   behavior) — see
   [docs/prd/04-subscribers-profiles.md](../prd/04-subscribers-profiles.md).

Steps 4-5 are skippable; you'll need at least one NAS and profile before the
PPPoE test in step 3 below, whether you create them here or later under
Subscribers/NAS in the panel.

**If the wizard UI isn't available yet in your build** (frontend still
landing), the same flow works directly against the API — see
[Appendix: wizard by API](#appendix-wizard-by-api) below. Every step above
has a 1:1 endpoint.

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
time from step 1's `sudo ./scripts/install.sh` to this passing test —
that's the number this document is held to (< 30 minutes).

## Appendix: wizard by API

Useful for rehearsing before the panel UI lands, or for scripting a repeat
install. All bodies are JSON; `$FP` is the fingerprint from step 1 below.

```sh
API=https://<this-server>/api/v1

# 1. Status + fingerprint
curl -s $API/setup/status
FP=$(curl -s $API/setup/license | grep -o '"fingerprint":"[^"]*"' | cut -d'"' -f4)

# (optional) upload a license blob a vendor issued for $FP
curl -s -X POST $API/setup/license -H 'Content-Type: application/json' -d @license.json

# 2. Create the admin
curl -s -X POST $API/setup/admin -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"choose-a-real-password"}'

# 3. Branding (optional)
curl -s -X POST $API/setup/branding -H 'Content-Type: application/json' \
  -d '{"name":"Your ISP"}'

# From here on, log in and use the normal authenticated API/panel for
# NAS + profile creation (setup/* refuses with setup_complete once step 2
# has run — that's not a bug, it's the point).
TOKEN=$(curl -s -X POST $API/auth/login -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"choose-a-real-password"}' \
  | grep -o '"access_token":"[^"]*"' | cut -d'"' -f4)
```

## Firewall / ports the stack uses

Host-published by `deploy/compose.yml`: **80/443 tcp** (panel/portal via
Caddy), **1812/1813 udp** (RADIUS auth/accounting), **3799 udp** (CoA), and
**5678 udp** (MikroTik MNDP neighbor discovery, v1.1 — broadcast discovery
only works when this server shares the routers' L2 segment; the panel's
IP-range scan works regardless). Nothing else is reachable from outside the
compose network.

## Factory reset (wipe all data, keep the install)

```sh
sudo hikrad factory-reset              # interactive: type 'factory reset' to confirm
sudo hikrad factory-reset --yes        # non-interactive
sudo hikrad factory-reset --no-backup  # skip the safety backup (explicit only)
```

Erases **all data** — subscribers, profiles, NAS, sessions, the ledger,
managers, and the installed license — and boots a fresh, empty system on the
same VM: the data directory is deleted (the append-only ledger/audit tables
make a fresh cluster the only clean zero state), the bind-mount targets are
recreated with the ownerships install.sh uses, the generated FreeRADIUS client
list is emptied, and the stack starts with a clean database. A safety backup is
taken first unless `--no-backup`; backups, `.env`, images, cron and the CLI are
all kept. Afterwards the panel runs the first-run wizard again (license, admin,
branding, NAS, profile).

## Uninstalling

```sh
sudo hikrad uninstall              # interactive: type 'uninstall hikrad' to confirm
sudo hikrad uninstall --keep-data  # remove the app but keep the data directory
sudo hikrad uninstall --purge      # remove EVERYTHING including backups + .env
```

Takes a final backup first (unless `--purge`), stops and deletes the
containers and images, removes the nightly-backup cron entry and the CLI
wrapper. Backups and `.env` are kept by default — the `.env` holds the backup
passphrase, without which kept backups can never be decrypted. Docker Engine
itself is never removed. `scripts/uninstall.sh` in a checkout does the same
when the CLI was never installed.

See also: [admin-guide.md](admin-guide.md), [pilot-checklist.md](pilot-checklist.md),
[backup-restore.md](backup-restore.md), [update.md](update.md),
[known-issues.md](known-issues.md).
