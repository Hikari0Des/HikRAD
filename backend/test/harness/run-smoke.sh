#!/usr/bin/env bash
# run-smoke.sh — native (no Docker) harness-smoke runner (v4-2, FR-118/119).
# Invoked by `make -C backend test-harness-smoke`. Expects a reachable
# PostgreSQL 16 + TimescaleDB and Redis (HIKRAD_DB_URL/HIKRAD_REDIS_URL env,
# same defaults as CI's service containers) and `freeradius` on PATH.
set -euo pipefail

BACKEND_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
REPO_DIR="$(cd "$BACKEND_DIR/.." && pwd)"

: "${HIKRAD_DB_URL:=postgres://hikrad:hikrad-ci@127.0.0.1:5432/hikrad_test?sslmode=disable}"
: "${HIKRAD_REDIS_URL:=redis://127.0.0.1:6379/0}"
export HIKRAD_DB_URL HIKRAD_REDIS_URL
export HIKRAD_ENCRYPTION_KEY="${HIKRAD_ENCRYPTION_KEY:-$(openssl rand -base64 32)}"
export HIKRAD_JWT_SECRET="${HIKRAD_JWT_SECRET:-$(openssl rand -base64 32)}"
export HIKRAD_ENV=dev

FR_DIR="/tmp/hikrad-harness-fr"
rm -rf "$FR_DIR" /tmp/hikrad-harness-fr-log /tmp/hikrad-harness-fr-run
cp -r "$REPO_DIR/deploy/freeradius" "$FR_DIR"
sed -i \
    -e "s#^raddbdir = /etc/freeradius#raddbdir = $FR_DIR#" \
    -e "s#^logdir = /var/log/freeradius#logdir = /tmp/hikrad-harness-fr-log#" \
    -e "s#^run_dir = \${localstatedir}/run/\${name}#run_dir = /tmp/hikrad-harness-fr-run#" \
    "$FR_DIR/radiusd.conf"
mkdir -p /tmp/hikrad-harness-fr-log /tmp/hikrad-harness-fr-run

# EAP certs are generated per install, never checked in (FR-127) — without
# them mods-enabled/eap fails to instantiate and FreeRADIUS exits at startup,
# so every harness case failed. Same generation step install.sh runs.
( cd "$FR_DIR/certs" && ./bootstrap ) >/tmp/hikrad-harness-certgen.log 2>&1 \
    || { cat /tmp/hikrad-harness-certgen.log >&2; echo "FreeRADIUS cert generation failed" >&2; exit 1; }
# FreeRADIUS reads the certs after dropping to radiusd.conf's user (freerad),
# so root-owned 0600 keys are unreadable — install.sh chowns them the same way.
if [ "$(id -u)" -eq 0 ] && getent passwd freerad >/dev/null 2>&1; then
    chown -R freerad:freerad "$FR_DIR/certs" /tmp/hikrad-harness-fr-log /tmp/hikrad-harness-fr-run
elif [ "$(id -u)" -ne 0 ]; then
    # Non-root (CI's runner user, a dev shell): FreeRADIUS can't switch to
    # freerad ("Cannot initialize supplementary group list … Operation not
    # permitted") and exits at startup — run it as the invoking user instead.
    sed -i -E 's/^([[:space:]]*)(user|group)[[:space:]]*=[[:space:]]*freerad/\1#\2 = freerad/' "$FR_DIR/radiusd.conf"
fi

export HIKRAD_RADIUS_CLIENTS_PATH="$FR_DIR/clients-generated.conf"

API_PID=""
FR_PID=""
cleanup() {
    [ -n "$FR_PID" ] && kill "$FR_PID" 2>/dev/null || true
    [ -n "$API_PID" ] && kill "$API_PID" 2>/dev/null || true
}
trap cleanup EXIT

echo "== starting hikrad-api =="
( cd "$BACKEND_DIR" && go run ./cmd/hikrad-api ) > /tmp/hikrad-harness-api.log 2>&1 &
API_PID=$!
for i in $(seq 1 30); do
    curl -fsS -o /dev/null http://127.0.0.1:8080/api/v1/system/version 2>/dev/null && break
    sleep 1
done

echo "== seeding demo data =="
( cd "$BACKEND_DIR" && go run ./cmd/hikrad-api seed )

echo "== starting FreeRADIUS =="
freeradius -f -l stdout -d "$FR_DIR" > /tmp/hikrad-harness-fr.log 2>&1 &
FR_PID=$!
sleep 3

echo "== running the packet harness =="
# -nas-ip must be the demo NAS `seed` registers (10.0.0.1, internal/seed) —
# the harness's own default (10.0.0.99) is unregistered, so the authorize
# engine's known-NAS check (FR-13.2) rejected every case as unknown_nas.
( cd "$BACKEND_DIR" && go run ./test/harness -addr 127.0.0.1:1812 -secret testing123 -nas-ip 10.0.0.1 )
