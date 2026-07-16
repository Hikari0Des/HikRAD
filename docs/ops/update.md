# Update runbook (FR-51.4/51.5)

```sh
hikrad update                    # online: pulls/builds latest, from a git checkout
hikrad update --bundle release.tar   # offline: loads images from a bundle first
```

The panel mirrors this runbook at **Settings > System** (v1.1): it shows the
installed version (from the repo-root `VERSION` file via the `HIKRAD_VERSION`
build arg) and walks the operator through these exact commands. A one-click
panel-triggered update is planned as v2 phase 5
(`docs/v2/07-one-click-updater.md`).

## What happens

1. **Automatic pre-update backup** (`hikrad backup now`, tagged `pre_update`
   in `backup_runs`) — this is the safety net if the update itself goes
   wrong in a way image rollback can't fix (a bad migration that already
   committed data changes, for instance).
2. Current image ids for `hikrad-api`/`hikrad-acct`/`hikrad-monitor` are
   recorded for rollback.
3. New images are obtained: `git pull --ff-only` + `docker compose build`
   (online path), or `docker load` from the `--bundle` tarball (offline
   path — for sites without reliable internet, NFR-7/FR-51.5).
4. `docker compose up -d` — `hikrad-api` runs pending migrations
   automatically on boot (forward-only; there is no down-migration path in
   production).
5. Waits for `hikrad-api`'s Docker healthcheck (up to 2 minutes) — this only
   passes once the process has completed migrations, connected to
   Postgres/Redis, and opened its port, so "healthy" means the update
   actually landed, not just that a container exists.
6. **If step 4 or 5 fails**: the previous image ids are re-tagged onto the
   compose-managed image names and the stack is brought back up on the old
   version. The command then exits non-zero with a pointer back to the
   pre-update backup from step 1, in case a partially-applied migration
   needs a full data rollback (image rollback alone fixes "the new code
   doesn't work"; it doesn't undo a migration that already committed —
   that's what the backup is for).

## Offline bundles

For sites without reliable internet (a real scenario for Iraqi ISPs outside
major cities, NFR-7), updates also ship as a versioned bundle: image
tarballs (`docker save` output) plus a manifest, distributed however you
already move files to the site (USB, a download portal, etc.). `hikrad
update --bundle <path>` loads it in place of pulling/building.

## Before you run a real update

- Confirm the pre-update backup path has free disk (backups fail loudly on
  disk pressure, they don't silently skip and let you update blind).
- If this update includes a schema change you know is risky, consider
  rehearsing it on a copy of production data first (`hikrad backup now` on
  prod, `hikrad restore` onto a scratch VM, update the scratch VM).
- Read the release notes for the version you're updating to — a major
  version bump may exceed your license's entitled-version field even though
  the update mechanically succeeds (license enforcement of major-version
  entitlement beyond that is a post-v1 item, master PRD Decision 2).
