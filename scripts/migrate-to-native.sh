#!/usr/bin/env bash
# migrate-to-native.sh — one-way migration path for an existing Docker
# Compose HikRAD install onto the v4-2 native systemd install (FR-118 §3.5).
# Invoked as `hikrad migrate-to-native` (scripts/hikrad wraps this file).
#
# GATE ITEM 12 (docs/v4/phases/phase-v4-2-native-install/00-phase.md §5):
# "Rehearsed on a restored pilot backup before any customer runs it." This
# script has NOT been rehearsed against real Compose-install data as of this
# commit — see docs/history/gates/v4-2.md. Do not run this against a
# customer's only copy of production data without first rehearsing it on a
# restored backup on a scratch VM, per that gate item.
#
# What it does, in order (stops touching things the moment anything fails):
#   1. Locate the old Compose install (default /opt/hikrad; override with
#      --old-root) and its running `postgres` container.
#   2. Dump the database and copy the old .env (needed: HIKRAD_ENCRYPTION_KEY
#      decrypts every subscriber/NAS secret already in that dump — losing it
#      makes the migrated data permanently unreadable, same stakes as
#      docs/ops/backup-restore.md's passphrase warning).
#   3. Stop the old Compose stack (frees :5432/:80/:443 for the native
#      services).
#   4. Run scripts/install.sh --no-start (provisions native Postgres/
#      FreeRADIUS/nginx/units without starting anything yet).
#   5. Patch the freshly generated /etc/hikrad/hikrad.env: HIKRAD_ENCRYPTION_
#      KEY and HIKRAD_BACKUP_PASSPHRASE are overwritten with the OLD values
#      (every other secret — DB password, JWT signing key, updater token —
#      is fine to be fresh; nothing on disk was encrypted with those).
#   6. Restore the dump into the native PostgreSQL, run migrations, start
#      services, verify.
#   7. Only on success, and only with --yes or after an interactive
#      confirmation, stop and remove the OLD containers/images. The old data
#      directory is NEVER deleted by this script — remove it yourself once
#      you've confirmed the native install is good.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
OLD_ROOT="/opt/hikrad"
YES=0

while [ $# -gt 0 ]; do
    case "$1" in
        --old-root) OLD_ROOT="${2:?--old-root requires a path}"; shift 2 ;;
        --yes) YES=1; shift ;;
        -h|--help) sed -n '2,40p' "$0"; exit 0 ;;
        *) echo "error: unknown argument '$1'" >&2; exit 1 ;;
    esac
done

log()  { printf '[migrate-to-native] %s\n' "$*"; }
die()  { printf '[migrate-to-native] ERROR: %s\n' "$*" >&2; exit 1; }

[ "$(id -u)" -eq 0 ] || die "must run as root"
command -v docker >/dev/null 2>&1 || die "docker not found — is there really an existing Compose install to migrate?"

OLD_ENV="$OLD_ROOT/.env"
OLD_COMPOSE="$OLD_ROOT/release/compose.yml"
[ -f "$OLD_COMPOSE" ] || OLD_COMPOSE="$OLD_ROOT/deploy/compose.yml"
[ -f "$OLD_ENV" ] || die "no old .env found at $OLD_ENV — pass --old-root <path> if the old install lives elsewhere"
[ -f "$OLD_COMPOSE" ] || die "no old compose.yml found under $OLD_ROOT — pass --old-root <path>"

old_env_get() { grep "^$1=" "$OLD_ENV" 2>/dev/null | tail -n1 | cut -d= -f2-; }
OLD_ENCRYPTION_KEY="$(old_env_get HIKRAD_ENCRYPTION_KEY)"
OLD_BACKUP_PASSPHRASE="$(old_env_get HIKRAD_BACKUP_PASSPHRASE)"
OLD_PG_USER="$(old_env_get POSTGRES_USER)"; [ -n "$OLD_PG_USER" ] || OLD_PG_USER=hikrad
OLD_PG_DB="$(old_env_get POSTGRES_DB)"; [ -n "$OLD_PG_DB" ] || OLD_PG_DB=hikrad
[ -n "$OLD_ENCRYPTION_KEY" ] || die "old .env has no HIKRAD_ENCRYPTION_KEY — refusing to migrate data that would come out permanently unreadable"

old_compose() { docker compose --env-file "$OLD_ENV" -f "$OLD_COMPOSE" "$@"; }

if [ "$YES" != 1 ]; then
    [ -t 0 ] || die "refusing to run non-interactively without --yes"
    cat <<EOF
This migrates the Compose install at $OLD_ROOT onto a native systemd
install on THIS host. It stops the old stack, dumps its database, installs
the native stack, and restores the data into it. The old data directory is
never deleted automatically.

GATE ITEM 12: this path has not been rehearsed against real production data
in this repo's history yet — rehearse on a restored backup first (see
docs/history/gates/v4-2.md before running this against a live customer).

Type 'migrate to native' to continue:
EOF
    read -r answer
    [ "$answer" = "migrate to native" ] || { log "Aborted. Nothing changed."; exit 0; }
fi

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

log "Starting the old stack's postgres only…"
old_compose up -d postgres
for i in $(seq 1 30); do
    old_compose exec -T postgres pg_isready -U "$OLD_PG_USER" -d "$OLD_PG_DB" >/dev/null 2>&1 && break
    sleep 2
done

log "Dumping the old database ($OLD_PG_DB)…"
old_compose exec -T postgres pg_dump -U "$OLD_PG_USER" -Fc -d "$OLD_PG_DB" > "$WORK/db.pgdump" \
    || die "pg_dump against the old stack failed — nothing else was touched"

log "Stopping the old Compose stack…"
old_compose down

log "Running the native installer (--no-start)…"
bash "$SCRIPT_DIR/install.sh" --no-start \
    || die "native install failed — the old stack is stopped but its data directory ($OLD_ROOT) is untouched; investigate before retrying"

ETC_DIR="/etc/hikrad"
ENV_FILE="$ETC_DIR/hikrad.env"
log "Preserving the old encryption key and backup passphrase in $ENV_FILE…"
sed -i \
    -e "s#^HIKRAD_ENCRYPTION_KEY=.*#HIKRAD_ENCRYPTION_KEY=${OLD_ENCRYPTION_KEY}#" \
    -e "s#^HIKRAD_BACKUP_PASSPHRASE=.*#HIKRAD_BACKUP_PASSPHRASE=${OLD_BACKUP_PASSPHRASE}#" \
    "$ENV_FILE"

PG_USER="$(grep '^POSTGRES_USER=' "$ENV_FILE" | cut -d= -f2-)"
PG_DB="$(grep '^POSTGRES_DB=' "$ENV_FILE" | cut -d= -f2-)"
PG_PASSWORD="$(grep '^POSTGRES_PASSWORD=' "$ENV_FILE" | cut -d= -f2-)"

log "Restoring into the native PostgreSQL…"
sudo -u postgres psql -v ON_ERROR_STOP=1 -c "DROP DATABASE IF EXISTS \"$PG_DB\";"
sudo -u postgres psql -v ON_ERROR_STOP=1 -c "CREATE DATABASE \"$PG_DB\" OWNER \"$PG_USER\";"
sudo -u postgres psql -v ON_ERROR_STOP=1 -d "$PG_DB" -c "CREATE EXTENSION IF NOT EXISTS timescaledb;"
PGPASSWORD="$PG_PASSWORD" psql -h 127.0.0.1 -U "$PG_USER" -d "$PG_DB" -v ON_ERROR_STOP=1 -c "SELECT timescaledb_pre_restore();"
PGPASSWORD="$PG_PASSWORD" pg_restore -h 127.0.0.1 -U "$PG_USER" -d "$PG_DB" --no-owner --no-privileges < "$WORK/db.pgdump" \
    || die "pg_restore failed — the native stack is not yet started; investigate the dump before starting it (old data directory is still untouched)"
PGPASSWORD="$PG_PASSWORD" psql -h 127.0.0.1 -U "$PG_USER" -d "$PG_DB" -v ON_ERROR_STOP=1 -c "SELECT timescaledb_post_restore();"

log "Starting the native stack…"
for u in hikrad-api hikrad-acct hikrad-monitor hikrad-radius-worker; do
    systemctl enable --now "$u" || die "$u failed to start — check: journalctl -u $u"
done
systemctl reload nginx 2>/dev/null || systemctl restart nginx

waited=0
until curl -fsS -o /dev/null "http://127.0.0.1:8080/api/v1/system/version" 2>/dev/null || [ "$waited" -ge 120 ]; do
    sleep 2; waited=$((waited + 2))
done
curl -fsS -o /dev/null "http://127.0.0.1:8080/api/v1/system/version" 2>/dev/null \
    || die "hikrad-api did not become healthy after migration — check: journalctl -u hikrad-api"

subs="$(PGPASSWORD="$PG_PASSWORD" psql -h 127.0.0.1 -U "$PG_USER" -d "$PG_DB" -tA -c 'SELECT count(*) FROM subscribers;' 2>/dev/null | tr -d '[:space:]')"
log "Migration complete. Verification: $subs subscriber row(s) present. Confirm this matches the old install, then test a RADIUS auth before doing anything destructive."
log "Old data directory kept at $OLD_ROOT — remove it yourself once you're confident, e.g.: rm -rf $OLD_ROOT"

if [ "$YES" = 1 ] || { [ -t 0 ] && read -r -p "Remove the old Docker containers/images now (data directory is kept)? [y/N] " ans && [ "${ans,,}" = "y" ]; }; then
    log "Removing old containers/images…"
    old_compose down --remove-orphans --rmi local 2>/dev/null || true
fi
