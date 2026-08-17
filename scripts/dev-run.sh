#!/usr/bin/env bash
# dev-run.sh — native dev inner loop (v4-2, FR-118: replaces `docker compose
# up` for local development). Runs hikrad-api/hikrad-acct/hikrad-monitor
# directly via `go run` against a LOCAL PostgreSQL 16 + TimescaleDB and Redis
# you already have running (see docs/ops/dev-setup.md for how to get those —
# apt on Linux, or WSL2 on Windows; this script does not install them).
#
# Usage:
#   ./scripts/dev-run.sh                 run the three Go services in the
#                                         foreground; Ctrl+C stops all of them
#   ./scripts/dev-run.sh --reset-db-only  drop + recreate the dev database
#                                         (does not start anything) — `make
#                                         dev-down`'s "how to reset" path
#
# FreeRADIUS is NOT started by this script — most panel/API/frontend work
# never needs it. To also exercise the RADIUS path locally, run FreeRADIUS
# separately pointed at deploy/freeradius (see docs/ops/dev-setup.md's
# "RADIUS round-trip" section) — the same instructions the native installer
# encodes in scripts/install.sh, just run by hand for a dev loop.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
ENV_FILE="$REPO_DIR/deploy/hikrad.env"

log() { printf '[dev-run] %s\n' "$*"; }
die() { printf '[dev-run] ERROR: %s\n' "$*" >&2; exit 1; }

[ -f "$ENV_FILE" ] || die "$ENV_FILE not found — run 'make dev-env' first"
set -a
# shellcheck disable=SC1090
. "$ENV_FILE"
set +a

pg_conn_args() {
    # Parses HIKRAD_DB_URL (postgres://user:pass@host:port/db?...) into
    # plain psql args — avoids a hard dependency on any URL-parsing tool.
    local url="${HIKRAD_DB_URL#postgres://}"
    local userpass="${url%%@*}"
    local rest="${url#*@}"
    local hostport="${rest%%/*}"
    PGUSER="${userpass%%:*}"
    PGPASSWORD="${userpass#*:}"
    PGHOST="${hostport%%:*}"
    PGPORT="${hostport#*:}"
    PGDATABASE="${rest#*/}"; PGDATABASE="${PGDATABASE%%\?*}"
    export PGUSER PGPASSWORD PGHOST PGPORT PGDATABASE
}

if [ "${1:-}" = "--reset-db-only" ]; then
    command -v psql >/dev/null 2>&1 || die "psql not found on PATH — install the postgresql-client package"
    pg_conn_args
    log "Dropping and recreating '$PGDATABASE' on $PGHOST:$PGPORT (all local dev data is discarded)…"
    psql -h "$PGHOST" -p "$PGPORT" -U "$PGUSER" -d postgres -v ON_ERROR_STOP=1 \
        -c "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = '$PGDATABASE' AND pid <> pg_backend_pid();" \
        -c "DROP DATABASE IF EXISTS \"$PGDATABASE\";" \
        -c "CREATE DATABASE \"$PGDATABASE\";"
    psql -h "$PGHOST" -p "$PGPORT" -U "$PGUSER" -d "$PGDATABASE" -v ON_ERROR_STOP=1 \
        -c "CREATE EXTENSION IF NOT EXISTS timescaledb;"
    log "Reset complete. 'make dev-up' (or 'make seed') will re-run migrations from scratch."
    exit 0
fi

command -v go >/dev/null 2>&1 || die "go not found on PATH"

PIDS=()
cleanup() {
    log "stopping…"
    for pid in "${PIDS[@]}"; do kill "$pid" 2>/dev/null || true; done
    wait 2>/dev/null || true
}
trap cleanup EXIT INT TERM

run_service() { # run_service NAME ./cmd/PACKAGE
    local name="$1" pkg="$2"
    ( cd "$REPO_DIR/backend" && go run "$pkg" 2>&1 | sed "s/^/[$name] /" ) &
    PIDS+=("$!")
}

log "Starting hikrad-api, hikrad-acct, hikrad-monitor (go run — first start is slower, compiles each package)…"
run_service api ./cmd/hikrad-api
run_service acct ./cmd/hikrad-acct
run_service monitor ./cmd/hikrad-monitor

log "Panel/portal: run 'cd frontend/panel && npm run dev' / 'cd frontend/portal && npm run dev' separately (Vite dev server, hot reload)."
log "API: http://127.0.0.1:8080/api/v1  |  Ctrl+C to stop everything."
wait
