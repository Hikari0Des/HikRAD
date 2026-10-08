# Install guide

Three ways to install HikRAD. Pick the one that matches your situation;
each is a copy-paste recipe. All three end at the same place: the panel
open in a browser, ready for the first-run wizard.

| You are… | Use |
|---|---|
| Installing for a **customer** (their server, no source access) | [A. Offline install from a release](#a-customer-install-offline-two-files) |
| Installing for a customer whose server **has internet**, once releases are published online | [B. One-line online install](#b-customer-install-online-one-command) |
| Installing **your own** server from the source repo | [C. Install from source](#c-your-own-server-from-source) |

## Before you start

- A clean **Ubuntu 22.04 / 24.04** or **Debian 12 / 13** server (VM or bare
  metal), with root or `sudo`. Other modern Linux works with a warning.
- Recommended: 4 vCPU, 8 GB RAM, 200 GB SSD. Smaller is fine for a lab.
- Internet access **during the install** so the server can install
  PostgreSQL, FreeRADIUS, nginx and Redis from the OS/vendor package
  repositories. Once installed, HikRAD runs fully offline.
- Ports reachable from your routers: **1812/udp** (RADIUS auth),
  **1813/udp** (accounting). From your staff/subscribers: **443/tcp**
  (and 80/tcp, which redirects to 443). HikRAD sends CoA to the routers on
  **3799/udp**.
- Nothing else listening on port 80/443. If there is, the installer asks for
  another port (or set `HIKRAD_HTTP_PORT=8880`).

---

## A. Customer install (offline, two files)

You receive (or build with [`make release`](release-checklist.md)) two files:

```
hikrad-vX.Y.Z.tar     the signed release bundle
install.sh            the installer
```

Copy **both** into the same folder on the customer's server (scp, USB, …),
then:

```sh
cd /path/to/that/folder
sudo bash install.sh
```

That's it. The installer checks the bundle's signature **before changing
anything**, then installs everything and prints the panel address. No git,
Go, Node or source code ever touches the customer's server. A tampered or
corrupted bundle is refused and nothing is changed.

## B. Customer install (online, one command)

Once a release has been published to the public release page (see
[release-checklist.md](release-checklist.md) → "Publishing online"):

```sh
curl -fsSL https://raw.githubusercontent.com/Hikari0Des/hikrad-releases/main/install.sh | sudo bash
```

This downloads the latest signed bundle and runs the same installer as A.
Until the first release is published online, this URL returns 404 — use A.

## C. Your own server from source

Needs access to the private source repo (a GitHub account or token with
access). Go and Node.js are installed automatically if missing.

```sh
sudo apt-get update && sudo apt-get install -y git
git clone -b master https://github.com/Hikari0Des/HikRAD.git hikrad
cd hikrad
sudo ./scripts/install.sh
```

Takes about 5–10 minutes (it compiles the backend and builds the panel).

---

## Options (all three methods)

Add these after the command (for B: `… | sudo bash -s -- --domain …`):

| Option | What it does |
|---|---|
| `--domain panel.example.isp` | Gets a real Let's Encrypt certificate (the domain's DNS must already point at this server). Without it a self-signed certificate is used. |
| `--no-start` | Installs everything but doesn't start the services. |

Environment variables for unattended installs: `HIKRAD_HTTP_PORT`
(alternative web port), `HIKRAD_DB_PASS` (pin the database password),
`HIKRAD_CF_TOKEN` (Cloudflare tunnel token). Every question the installer
could ask is asked before it changes anything.

## When the installer finishes

It prints a summary like:

```
 Panel:                https://192.168.1.50/
 Subscriber portal:    https://192.168.1.50/portal/
 Backup passphrase:    ………………
```

1. **Write down the backup passphrase.** It is shown once. It decrypts
   every backup; without it (and without `/etc/hikrad/hikrad.env`) backups
   can't be restored. See [backup-restore.md](backup-restore.md).
2. **Open the panel address** in a browser. With the default self-signed
   certificate, accept the browser warning once.
3. **Complete the first-run wizard:** license (the wizard shows this
   server's fingerprint if you need to request a license; the system runs
   while unlicensed), admin account, branding, first NAS, first profile.
   There is no default password: until the wizard is done, nobody can log in.
4. **Add each MikroTik** in the panel (NAS → Add). Its setup wizard can
   configure the router for you. Manually, on the router: a RADIUS client
   pointing at this server (auth 1812, acct 1813, the secret you set in the
   panel), and `/radius incoming set accept=yes` for CoA. A new NAS works
   immediately; nothing needs restarting.
5. **Check health** any time:

   ```sh
   sudo hikrad doctor
   ```

   Every line should say `[OK]`.

## Prove it works: a PPPoE login

Create a test subscriber (Subscribers → New) on the profile from the wizard
and dial it from a PPPoE client behind your MikroTik. **Live sessions**
shows it within seconds. Without a router at hand, a server with the source
checkout can simulate one:

```sh
cd backend
go run ./test/harness -addr <server-ip>:1812 -secret <the NAS secret> -nas-ip <the NAS IP you registered>
```

## Updating later

See [update.md](update.md). In short: copy the new `hikrad-vX.Y.Z.tar` to
the server and run `sudo hikrad update --bundle hikrad-vX.Y.Z.tar` (or click
**Update now** in Settings → System). Source installs: `git pull`, then
`sudo hikrad update`.

## Troubleshooting

| Symptom | Fix |
|---|---|
| `hikrad: unknown command 'doctor'` | An older `hikrad` CLI from a previous install is still on the server. Finish (or re-run) the installer; it replaces the CLI. |
| Installer asks for a port | Something already uses port 80. Pick another (e.g. 8880); it's remembered for future runs. |
| `hikrad doctor` shows a failed check | `journalctl -u <service name> -n 50` shows why. Re-running the installer is always safe: it never regenerates secrets or changes the port. |
| Panel shows **502 Bad Gateway** | hikrad-api isn't running. `journalctl -u hikrad-api -n 50`. |
| A router's logins are rejected with `unknown_nas` | The NAS IP in the panel doesn't match the IP the router sends from. Fix it in the panel; no restart needed. |
| `bundle verification failed` | The tar is corrupted or modified. Copy it again from the original source. |

## Where things live

| What | Where |
|---|---|
| Programs | `/opt/hikrad/bin`, panel files in `/opt/hikrad/www` |
| Configuration and secrets | `/etc/hikrad/hikrad.env` (keep a safe copy) |
| FreeRADIUS config | `/etc/hikrad/freeradius` |
| Data | `/var/lib/hikrad`, PostgreSQL database `hikrad` |
| Backups (nightly, 03:00) | `/var/backups/hikrad` |
| Logs | journald: `journalctl -u hikrad-api` (also `hikrad-acct`, `hikrad-monitor`, `hikrad-radius-worker`) |

Re-running the installer upgrades in place and is always safe: secrets in
`hikrad.env` are never regenerated and the chosen port never changes.

## What the installer does

1. Checks the OS; for a bundle, verifies its signature before anything else.
2. Installs PostgreSQL 16 + TimescaleDB (a server that already has a
   PostgreSQL cluster keeps its version; override with `HIKRAD_PG_MAJOR`),
   Redis, FreeRADIUS 3, nginx; creates the `hikrad` database.
3. Creates the service users and directories (on every run, so upgrades
   heal ownership too) and generates `/etc/hikrad/hikrad.env` once.
4. Installs the programs, the panel/portal, the migrations and the
   FreeRADIUS config; validates the FreeRADIUS and nginx configs before
   enabling anything.
5. Adds an nginx site (other sites are left alone; on a server where the
   installer itself installed nginx, nginx's placeholder page is disabled
   so the panel answers on the server's IP).
6. Installs and starts the `hikrad-*` systemd services and a nightly backup
   timer, waits until the API answers, and prints the summary.

## Factory reset and uninstall

```sh
sudo hikrad factory-reset       # wipe all data, keep the install (takes a safety backup first)
sudo hikrad uninstall           # remove HikRAD (keeps backups and hikrad.env)
sudo hikrad uninstall --purge   # remove everything, including backups
```

PostgreSQL, Redis, nginx and FreeRADIUS packages are never removed — they
may serve other things.

## Migrating an old Docker Compose install

`sudo hikrad migrate-to-native` — see [update.md](update.md).

See also: [admin-guide.md](admin-guide.md),
[pilot-checklist.md](pilot-checklist.md),
[backup-restore.md](backup-restore.md), [update.md](update.md),
[known-issues.md](known-issues.md).
