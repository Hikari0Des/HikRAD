# Update runbook (FR-51.4/51.5, bundle verification FR-81.3 — v2 phase 5)

```sh
hikrad update --bundle hikrad-vX.Y.Z.tar   # production: verify + load a signed offline bundle
hikrad update                              # dev-only source checkouts: git pull + docker compose build
```

The panel mirrors this runbook at **Settings > System** (v1.1): it shows the
installed version (from `GET /api/v1/system/version`) and walks the operator
through these exact commands. A one-click panel-triggered update is planned
as v2 phase 7 (`docs/v2/07-one-click-updater.md`), which will build on this
phase's bundle/registry plumbing rather than the reverse.

## What happens

1. **Automatic pre-update backup** (`hikrad backup now`, tagged `pre_update`
   in `backup_runs`) — this is the safety net if the update itself goes
   wrong in a way image rollback can't fix (a bad migration that already
   committed data changes, for instance).
2. Current image ids for `hikrad-api`/`hikrad-acct`/`hikrad-monitor` are
   recorded for rollback, keyed by each image's own actual tag (not a
   hardcoded name) — correct whether this install's images are the pinned
   `ghcr.io/hikrad/...` tags a bundle carries or the `build:`-synthesized
   name a dev checkout uses.
3. New images are obtained:
   - `--bundle <path>` (production): the tarball is **verified — checksums
     and an Ed25519 signature against an embedded public key — before
     anything in it is used** (same `scripts/verify-bundle.sh` `install.sh`
     itself calls). A failed verification refuses immediately and changes
     nothing. On success, its images are `docker load`ed and its
     `compose.yml`/`freeradius/`/`caddy/`/`scripts/`/`migrations/` are
     staged into `/opt/hikrad/release/` — replacing the previous release's
     copy only now, with the previous copy kept as a one-deep rollback
     backup (see step 6).
   - No `--bundle` (dev-only source checkout): `git pull --ff-only` +
     `docker compose build`, exactly as before.
4. `docker compose up -d` — `hikrad-api` runs pending migrations
   automatically on boot (forward-only; there is no down-migration path in
   production).
5. Waits for `hikrad-api`'s Docker healthcheck (up to 2 minutes) — this only
   passes once the process has completed migrations, connected to
   Postgres/Redis, and opened its port, so "healthy" means the update
   actually landed, not just that a container exists.
6. **If step 4 or 5 fails**: the previous image ids are re-tagged back onto
   their own tags, and — for a `--bundle` update specifically — the previous
   `/opt/hikrad/release/` contents are restored too (a bundle pins an exact
   version tag per release, so image-id rollback alone can't rescue a failed
   update unless `compose.yml` itself reverts along with it). The stack is
   brought back up on the old version, and the command exits non-zero with a
   pointer back to the pre-update backup from step 1, in case a
   partially-applied migration needs a full data rollback (image/file
   rollback alone fixes "the new code doesn't work"; it doesn't undo a
   migration that already committed — that's what the backup is for).

## Offline bundles (FR-81.2, NFR-7)

Every release ships as a signed `hikrad-vX.Y.Z.tar`: HikRAD's own 4 images
plus the pinned third-party images the stack needs (Postgres/TimescaleDB,
Redis, FreeRADIUS — a bundle install/update needs no internet access at all,
not even to pull a base image), a rendered `compose.yml`, runtime config, the
installer scripts themselves, and migrations — checksummed and signed (see
[release-checklist.md](release-checklist.md)). Distributed however you
already move files to a site without reliable internet (a real scenario for
Iraqi ISPs outside major cities): USB, a download portal, etc. `hikrad update
--bundle <path>` verifies and loads it; `install.sh --bundle <path>` does the
same for a first install (see [install-guide.md](install-guide.md)).

A registry-pull mode (`ghcr.io/hikrad/*`) also exists for the vendor's own
use, but the signed bundle is the only supported customer-facing delivery
path in this phase — no registry credential is issued to customers.

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
