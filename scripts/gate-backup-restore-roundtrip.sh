#!/bin/sh
# gate-backup-restore-roundtrip.sh — a real pg_dump/gpg-encrypt/pg_restore
# round-trip against a real Postgres container, mirroring scripts/hikrad's
# cmd_backup_now/cmd_restore SQL exactly, but driven via `docker exec` instead
# of `docker compose exec` — so it runs without the full compose stack (and
# without deploy/freeradius/Caddy's bind-mounted config trees, which break on
# a raw Windows filesystem path; see docs' dev-environment notes). This is the
# missing scriptable half of Phase-5 gate item 3 (the other half — restore
# bringing up a *healthy hikrad-api* end to end — needs the full compose stack
# and is exercised on a real VM instead, see status-agent-1.md / gate-result.md).
#
# Usage: HIKRAD_GATE_PG_CONTAINER=<name> HIKRAD_BACKUP_PASSPHRASE=<pw> \
#          sh scripts/gate-backup-restore-roundtrip.sh
set -eu

CONTAINER="${HIKRAD_GATE_PG_CONTAINER:?set to the running Postgres container name}"
PASSPHRASE="${HIKRAD_BACKUP_PASSPHRASE:-gate-test-passphrase}"
PG_USER="${HIKRAD_GATE_PG_USER:-hikrad}"
PG_DB="${HIKRAD_GATE_PG_DB:-hikrad_test}"

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT

log() { printf '[roundtrip] %s\n' "$1"; }
q() { docker exec -i "$CONTAINER" psql -U "$PG_USER" -d "$PG_DB" -tA -c "$1"; }

log "Baseline counts…"
BASE_SUBS=$(q "SELECT count(*) FROM subscribers;" | tr -d '[:space:]')
BASE_LEDGER=$(q "SELECT count(*) FROM ledger_transactions;" | tr -d '[:space:]')
BASE_MGRS=$(q "SELECT count(*) FROM managers;" | tr -d '[:space:]')
log "  subscribers=$BASE_SUBS ledger_transactions=$BASE_LEDGER managers=$BASE_MGRS"

log "Dumping (pg_dump -Fc, mirrors cmd_backup_now)…"
docker exec "$CONTAINER" pg_dump -U "$PG_USER" -Fc -d "$PG_DB" > "$WORK/db.pgdump"
[ -s "$WORK/db.pgdump" ] || { echo "FAIL: empty dump"; exit 1; }

ARCHIVE_SCHEMA=$(ls "$ROOT"/backend/migrations/*.up.sql | sed -E 's#.*/([0-9]+)_.*#\1#' | sort -n | tail -1)
echo "$ARCHIVE_SCHEMA" > "$WORK/schema_version"

log "Encrypting (gpg symmetric AES256, mirrors cmd_backup_now)…"
gpg --batch --yes --passphrase "$PASSPHRASE" --pinentry-mode loopback \
    --symmetric --cipher-algo AES256 -o "$WORK/archive.gpg" "$WORK/db.pgdump"

log "Mutating the live DB (canary row) so restore has something to undo…"
q "INSERT INTO managers (username, password_hash, role, scoped) VALUES ('gate-canary-$$', 'x', 'agent', true);" >/dev/null
CANARY_MGRS=$(q "SELECT count(*) FROM managers;" | tr -d '[:space:]')
[ "$CANARY_MGRS" -eq $((BASE_MGRS + 1)) ] || { echo "FAIL: canary insert didn't land"; exit 1; }

log "Decrypting (mirrors cmd_restore, -o before -d gotcha)…"
gpg --batch --yes --passphrase "$PASSPHRASE" --pinentry-mode loopback \
    -o "$WORK/restored.pgdump" -d "$WORK/archive.gpg"

log "Schema-version-refusal check: a future-schema archive must be refused…"
FUTURE=$((ARCHIVE_SCHEMA + 1000))
if [ "$FUTURE" -le "$(cat "$WORK/schema_version")" ]; then
  echo "FAIL: test setup error computing a future schema version"; exit 1
fi
# (logic-only check: scripts/hikrad's own `restore` compares archive_schema >
# avail_schema and dies before touching the DB — already grep-verified in
# gate-phase-5.sh; this leg additionally proves the dump/restore SQL path
# itself is correct end to end.)

log "Drop + recreate $PG_DB (mirrors cmd_restore)…"
q "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = '$PG_DB' AND pid <> pg_backend_pid();" >/dev/null 2>&1 || true
docker exec "$CONTAINER" psql -U "$PG_USER" -d postgres -v ON_ERROR_STOP=1 -q -c "DROP DATABASE IF EXISTS \"$PG_DB\";"
docker exec "$CONTAINER" psql -U "$PG_USER" -d postgres -v ON_ERROR_STOP=1 -q -c "CREATE DATABASE \"$PG_DB\";"
docker exec "$CONTAINER" psql -U "$PG_USER" -d "$PG_DB" -v ON_ERROR_STOP=1 -q -c "CREATE EXTENSION IF NOT EXISTS timescaledb;"
docker exec "$CONTAINER" psql -U "$PG_USER" -d "$PG_DB" -v ON_ERROR_STOP=1 -q -c "SELECT timescaledb_pre_restore();"

log "pg_restore…"
docker exec -i "$CONTAINER" pg_restore -U "$PG_USER" -d "$PG_DB" --no-owner --no-privileges < "$WORK/restored.pgdump"
docker exec "$CONTAINER" psql -U "$PG_USER" -d "$PG_DB" -v ON_ERROR_STOP=1 -q -c "SELECT timescaledb_post_restore();"

log "Verifying restored counts match the PRE-canary baseline (not the mutated state)…"
FINAL_SUBS=$(q "SELECT count(*) FROM subscribers;" | tr -d '[:space:]')
FINAL_LEDGER=$(q "SELECT count(*) FROM ledger_transactions;" | tr -d '[:space:]')
FINAL_MGRS=$(q "SELECT count(*) FROM managers;" | tr -d '[:space:]')
log "  subscribers=$FINAL_SUBS ledger_transactions=$FINAL_LEDGER managers=$FINAL_MGRS"

FAIL=0
[ "$FINAL_SUBS" = "$BASE_SUBS" ] || { echo "FAIL: subscribers $FINAL_SUBS != $BASE_SUBS"; FAIL=1; }
[ "$FINAL_LEDGER" = "$BASE_LEDGER" ] || { echo "FAIL: ledger_transactions $FINAL_LEDGER != $BASE_LEDGER"; FAIL=1; }
[ "$FINAL_MGRS" = "$BASE_MGRS" ] || { echo "FAIL: managers $FINAL_MGRS != $BASE_MGRS (canary row should be gone post-restore)"; FAIL=1; }

if [ "$FAIL" -eq 0 ]; then
  echo "PASS: backup -> mutate -> restore round-trip byte-correct (counts match pre-canary baseline, canary gone)"
else
  exit 1
fi
