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
( cd "$BACKEND_DIR" && go run ./test/harness -addr 127.0.0.1:1812 -secret testing123 )
