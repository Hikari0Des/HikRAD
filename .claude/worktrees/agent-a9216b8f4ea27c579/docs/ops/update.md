# Update runbook (FR-51.4/51.5, bundle verification FR-81.3, retargeted onto
# systemd in v4-2/FR-121)

```sh
hikrad update --bundle hikrad-vX.Y.Z.tar   # production: verify + stage a signed offline bundle
hikrad update                              # dev-only source checkouts: rebuild + restage from source
```

The panel mirrors this runbook at **Settings > System**: it shows the
installed version (from `GET /api/v1/system/version`) and walks the operator
through these exact commands — always available, regardless of whether the
one-click path below is provisioned on this install.

## One-click update from the panel

Settings > System also gains **Check for update** / **Update now** for a
manager holding the `system.update` permission (admin by default). This
does not replace the commands above — it's the exact same `hikrad update`
path, triggered from a browser instead of a terminal, with live progress.

**How it works:** `hikrad-updaterd` runs directly on the server as a
systemd unit (never inside a container — it has to be able to update every
other unit, including any that would host a caller talking to it) and
listens on a local unix socket (`/run/hikrad/updater.sock`), never a
network port. `hikrad-api` relays the panel's requests to it over that
socket, authenticated by a shared token (`HIKRAD_UPDATER_TOKEN` in
`/etc/hikrad/hikrad.env`). **The daemon's own Go code did not change for
v4-2** — it has always worked by shelling into `hikrad update` as a child
process and scanning its output for stage markers; v4-2 only retargeted
what `hikrad update` itself *drives* (`systemctl`/`install.sh` instead of
`docker compose`), which the daemon observes exactly the same way it
always did.

**A lock file (`/var/lib/hikrad/data/updater/update.lock`, held by
whichever `hikrad update` invocation is actually running) means a
CLI-triggered and a panel-triggered update can never run at the same
time** — whichever gets there first wins, the other is refused immediately
with a "locked" message rather than queued. This survives the daemon
itself dying mid-update: the lock is held by the child process's own file
descriptor, not by the daemon.

**Checking for an update never touches the network** (NFR-7): the daemon
scans a bundle-drop directory, `/var/lib/hikrad/incoming/`, for the
highest `hikrad-vX.Y.Z.tar` newer than the running version. Get the file
onto the server however you already move files there without reliable
internet (`scp`, USB, a download portal) into that directory, *then* click
"Check for update" in the panel.

## What happens

1. **Automatic pre-update backup** (`hikrad backup now`, tagged
   `pre_update` in `backup_runs`) — this is the safety net if the update
   itself goes wrong in a way a binary rollback can't fix (a bad migration
   that already committed data changes, for instance).
2. The bundle is verified (checksums + Ed25519 signature) before anything
   in it is used — a failed verification refuses immediately and changes
   nothing.
3. The current `/opt/hikrad/bin`, `/opt/hikrad/www`, and
   `/etc/hikrad/freeradius` are copied to a one-deep rollback location
   (`/opt/hikrad/release.rollback`) — there is no container image id to
   re-tag without Docker, so this is v4-2's equivalent rollback mechanism.
4. `scripts/install.sh` runs (source or `--bundle`) to stage the new
   release in place: new binaries, frontend build, FreeRADIUS config,
   migrations — the exact same idempotent, non-secret-touching install
   path a fresh install uses.
5. Every `hikrad-*` unit is restarted; `hikrad-api` runs pending migrations
   automatically on boot (forward-only; there is no down-migration path in
   production).
6. Waits for `hikrad-api` to answer `GET /api/v1/system/version` (up to 2
   minutes) — this only succeeds once migrations have completed and the
   process has connected to Postgres/Redis and opened its port.
7. **If step 4, 5, or 6 fails**: the rollback location's binaries/www/
   FreeRADIUS config are restored, every unit is restarted on the old
   version, and the command exits non-zero with a pointer back to the
   pre-update backup from step 1, in case a partially-applied migration
   needs a full data rollback (binary/file rollback alone fixes "the new
   code doesn't work"; it doesn't undo a migration that already
   committed — that's what the backup is for). **This rollback completes
   even if `hikrad-updaterd` and `hikrad-api` are both dead** — it is pure
   `systemctl`/filesystem operations run by the `hikrad update` child
   process itself, with no dependency on either of them being alive.

## Offline bundles (FR-81.2, NFR-7)

Every release ships as a signed `hikrad-vX.Y.Z.tar`: HikRAD's own binaries
(`hikrad-api`, `hikrad-acct`, `hikrad-monitor`, `hikrad-updaterd`), the
built frontend, FreeRADIUS config, the nginx template, the systemd units,
the installer scripts themselves, and migrations — checksummed and signed
(see [release-checklist.md](release-checklist.md)). No images, no
registry pull — a bundle install/update needs no internet access at all.
Distributed however you already move files to a site without reliable
internet (a real scenario for Iraqi ISPs outside major cities): USB, a
download portal, etc.

## Migrating an existing Docker Compose install to native (v4-2, FR-118 §3.5)

```sh
sudo hikrad migrate-to-native            # interactive confirmation
sudo hikrad migrate-to-native --yes      # non-interactive
sudo hikrad migrate-to-native --old-root /opt/hikrad   # if the old install lives elsewhere
```

One-way, stops the old stack, dumps its database, runs the native
installer, restores the dump (preserving the old encryption key and backup
passphrase so existing encrypted data stays readable), starts the native
stack, verifies, and only then offers to remove the old containers/images.
**The old data directory is never deleted automatically** — remove it
yourself once you've confirmed the native install is good.

> **Gate item 12 (docs/v4/phases/phase-v4-2-native-install/00-phase.md):
> this path must be rehearsed on a restored pilot backup before any real
> customer install runs it.** That rehearsal has not happened as of this
> commit — see `docs/history/gates/v4-2.md`. Do not run this against a
> customer's only copy of production data without rehearsing it on a
> restored backup on a scratch VM first.

## Before you run a real update

- Confirm the pre-update backup path has free disk (backups fail loudly on
  disk pressure, they don't silently skip and let you update blind).
- If this update includes a schema change you know is risky, consider
  rehearsing it on a copy of production data first (`hikrad backup now` on
  prod, `hikrad restore` onto a scratch VM, update the scratch VM).
- Read the release notes for the version you're updating to.
