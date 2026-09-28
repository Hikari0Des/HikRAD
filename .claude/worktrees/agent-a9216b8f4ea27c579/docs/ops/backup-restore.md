# Backup & restore runbook (FR-51.1–51.3)

## What's in a backup

One `hikrad-backup-<UTC timestamp>.tar.gz.gpg` file containing:
- `db.pgdump` — `pg_dump` custom format of the whole database, including
  TimescaleDB hypertables (usage points, usage_daily rollups).
- `.env` — every secret this install needs, including the AES-GCM key that
  decrypts subscriber RADIUS passwords and NAS secrets. **This is why `.env`
  must be in the backup**: without it, a restored database's encrypted
  columns are permanently unreadable, even by HikRAD itself.
- `caddy/` — the reverse-proxy config (matters if you customized it, e.g.
  for `--domain`/Let's Encrypt).
- `schema_version` — the migration version the database was at, used by
  restore's forward-compatibility check.

The whole archive is symmetrically encrypted with **`HIKRAD_BACKUP_PASSPHRASE`**
(gpg, AES-256) before it touches disk. A stolen archive alone decrypts
nothing — this is deliberate (sub-PRD 06 §7's open question, resolved this
phase): a copy of `.env`'s encryption key living inside every backup would
otherwise mean "stolen backup = data + key" for anyone who got hold of one.

**The passphrase itself lives in two places, on purpose:**
1. Printed once, at the end of `install.sh` — write it down.
2. `/opt/hikrad/.env` (`HIKRAD_BACKUP_PASSPHRASE=...`) — needed so the
   nightly cron backup can run unattended, with nobody typing a passphrase
   at 3am.

**If you lose both**, existing backups are permanently unrecoverable. There
is no vendor escrow and no recovery mechanism — this is a deliberate
trade-off (a backdoor into every customer's encrypted backups would be worse)
documented here so it's a known trade-off, not a surprise.

## Taking a backup

```sh
hikrad backup now      # immediate, ad-hoc
hikrad backup list      # what's on disk, newest first
```

Nightly backups run automatically via the cron entry `install.sh` installs
(default 03:00 server time). Retention defaults to 14 archives
(`HIKRAD_BACKUP_RETENTION` in `.env`) — older archives are pruned after each
successful backup, never during a failed one (a disk-full backup fails
loudly and leaves existing archives alone; it does not corrupt rotation).

Each run is also recorded in the `backup_runs` table
(`GET /api/v1/backups`), which is what the panel's "last backup age" display
and staleness alert read.

## Restoring

```sh
hikrad restore hikrad-backup-20260101T030000Z.tar.gz.gpg
# or a full path; relative names are looked up under the backups directory
```

This is destructive to the target's current database — **rehearse it on a
second VM before you ever need it on production.**

What it does, step by step (FR-51.2):
1. Asks for confirmation (`type 'yes'`) unless `HIKRAD_RESTORE_YES=1`.
2. Decrypts the archive with `HIKRAD_BACKUP_PASSPHRASE`.
3. Checks the archive's embedded schema version against this install's
   available migrations. **Restoring into an older install (one that
   doesn't have all the migrations the archive expects) is refused** with a
   clear message — update first, then retry. Restoring into a newer or
   equal install proceeds; forward migrations run automatically when
   `hikrad-api` boots at the end.
4. Stops the whole stack, brings up only `postgres`.
5. Drops and recreates the target database, then restores the dump
   (including the TimescaleDB pre/post-restore steps hypertables need).
6. Replaces `.env` and the Caddy config with the archive's copies (the old
   `.env` is saved alongside as `.env.pre-restore.bak`, never deleted).
7. Starts the full stack and waits for `hikrad-api`'s healthcheck.
8. Prints a verification summary: subscriber count, last ledger entry time,
   last accounting record time. **Check these against what you expect** —
   this is the step that actually tells you the restore is trustworthy, not
   just that the commands didn't error.

### Same-hardware restore and licensing

Restoring a backup onto the *same* machine it came from never triggers a
license grace period (the fingerprint is unchanged). Restoring onto
*different* hardware (disaster recovery, a hardware refresh) does — see
[admin-guide.md#license](admin-guide.md#license). RADIUS keeps authenticating
throughout either way; only panel writes are affected, and only after the
full 14-day grace window.

### If restore fails partway

- Decrypt/schema-check failures happen before anything is touched — nothing
  to clean up, fix the input and retry.
- A `pg_restore` failure leaves the stack stopped with the *old* database
  already dropped — this is the one genuinely bad window. Do not `hikrad up`
  in this state; instead retry `hikrad restore` with a known-good archive,
  or restore the pre-update backup `hikrad update` would have taken if this
  restore was itself part of a rollback.
