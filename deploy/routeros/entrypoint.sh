#!/usr/bin/env bash
# entrypoint.sh — the RouterOS container's single top-level process (v4-2,
# FR-120). RouterOS's `/container` feature runs one process per container;
# this script supervises PostgreSQL, Redis, FreeRADIUS, nginx, and all three
# HikRAD Go services underneath it, restarting anything that dies, and
# shutting everything down cleanly on SIGTERM (container stop).
#
# EVERYTHING under $HIKRAD_DATA_DIR (the operator-mounted ext4 disk — see
# README.md) is the entire state of this install: the database, secrets,
# and the TLS cert. Removing the container app WITHOUT that disk mounted
# again destroys the database — this is documented, twice, in README.md,
# because it is the single most consequential fact about this target.
set -euo pipefail

DATA_DIR="${HIKRAD_DATA_DIR:-/hikrad-data}"
ENV_FILE="$DATA_DIR/hikrad.env"
FR_DIR="/tmp/hikrad-freeradius"
TLS_DIR="$DATA_DIR/tls"

log() { printf '[hikrad-routeros] %s\n' "$*"; }

mkdir -p "$DATA_DIR" "$DATA_DIR/postgres" "$DATA_DIR/redis" "$DATA_DIR/branding" "$DATA_DIR/acct-spill" "$TLS_DIR"

# --- secrets: generate once, persist on the mounted disk --------------------
if [ ! -f "$ENV_FILE" ]; then
    log "First boot: generating $ENV_FILE…"
    bash /opt/hikrad/gen-env.sh --env prod "$ENV_FILE"
fi
set -a
# shellcheck disable=SC1090
. "$ENV_FILE"
set +a
# Same-host services: point at loopback (mirrors scripts/install.sh).
export HIKRAD_DB_URL="postgres://hikrad:${POSTGRES_PASSWORD}@127.0.0.1:5432/hikrad?sslmode=disable"
export HIKRAD_REDIS_URL="redis://127.0.0.1:6379/0"
export HIKRAD_MIGRATIONS_DIR=/opt/hikrad/migrations
export HIKRAD_ACCT_SPILL_DIR="$DATA_DIR/acct-spill"
export HIKRAD_BRANDING_DIR="$DATA_DIR/branding"
export HIKRAD_RADIUS_CLIENTS_PATH="$FR_DIR/clients-generated.conf"

# --- PostgreSQL ---------------------------------------------------------------
chown -R postgres:postgres "$DATA_DIR/postgres" 2>/dev/null || true
if [ ! -f "$DATA_DIR/postgres/PG_VERSION" ]; then
    log "Initializing PostgreSQL data directory…"
    su -s /bin/sh postgres -c "initdb -D '$DATA_DIR/postgres' -U postgres" >/tmp/initdb.log 2>&1
fi
su -s /bin/sh postgres -c "pg_ctl -D '$DATA_DIR/postgres' -l /tmp/postgres.log -w start -o '-c listen_addresses=127.0.0.1'"
for i in $(seq 1 30); do
    pg_isready -h 127.0.0.1 -U postgres >/dev/null 2>&1 && break
    sleep 1
done

role_exists="$(su -s /bin/sh postgres -c "psql -tAc \"SELECT 1 FROM pg_roles WHERE rolname='hikrad'\"" 2>/dev/null || true)"
if [ "$role_exists" != "1" ]; then
    su -s /bin/sh postgres -c "psql -v ON_ERROR_STOP=1 -c \"CREATE ROLE hikrad LOGIN PASSWORD '${POSTGRES_PASSWORD}';\""
fi
db_exists="$(su -s /bin/sh postgres -c "psql -tAc \"SELECT 1 FROM pg_database WHERE datname='hikrad'\"" 2>/dev/null || true)"
if [ "$db_exists" != "1" ]; then
    su -s /bin/sh postgres -c "psql -v ON_ERROR_STOP=1 -c 'CREATE DATABASE hikrad OWNER hikrad;'"
fi
su -s /bin/sh postgres -c "psql -v ON_ERROR_STOP=1 -d hikrad -c 'CREATE EXTENSION IF NOT EXISTS timescaledb;'" || true

# --- Redis ---------------------------------------------------------------------
redis-server --daemonize yes --dir "$DATA_DIR/redis" --appendonly yes --appendfsync always \
    --bind 127.0.0.1 --logfile /tmp/redis.log

# --- FreeRADIUS: same retarget as scripts/install.sh -------------------------
rm -rf "$FR_DIR"
cp -r /etc/hikrad/freeradius "$FR_DIR"
sed -i \
    -e "s#^raddbdir = /etc/freeradius#raddbdir = $FR_DIR#" \
    -e "s#^logdir = /var/log/freeradius#logdir = /tmp/hikrad-radius-log#" \
    -e "s#^run_dir = \${localstatedir}/run/\${name}#run_dir = /tmp/hikrad-radius-run#" \
    "$FR_DIR/radiusd.conf"
mkdir -p /tmp/hikrad-radius-log /tmp/hikrad-radius-run
touch "$FR_DIR/clients-generated.conf"
chown hikrad-api:hikrad-api "$FR_DIR/clients-generated.conf" 2>/dev/null || true

# --- self-signed TLS (persisted) + nginx --------------------------------------
if [ ! -f "$TLS_DIR/cert.pem" ]; then
    openssl req -x509 -nodes -newkey rsa:2048 -days 3650 \
        -keyout "$TLS_DIR/key.pem" -out "$TLS_DIR/cert.pem" \
        -subj "/CN=hikrad" -addext "subjectAltName=DNS:localhost,IP:127.0.0.1" >/dev/null 2>&1
fi
mkdir -p /etc/hikrad/tls
ln -sf "$TLS_DIR/cert.pem" /etc/hikrad/tls/cert.pem
ln -sf "$TLS_DIR/key.pem" /etc/hikrad/tls/key.pem
sed -e "s/@@HTTP_PORT@@/8080_UNUSED/g" -e "s/@@HTTPS_PORT@@/443/g" \
    /etc/hikrad/nginx.conf.template > /tmp/hikrad-nginx.conf
# The RouterOS target intentionally serves HTTP only on :80 (no redirect) —
# TLS termination on a resource-constrained router is the operator's own
# WireGuard/reverse-proxy choice, not this all-in-one image's job. Strip
# the generated file's https-redirect/ssl server block down to one plain
# HTTP server reusing the same location rules.
mkdir -p /etc/nginx/http.d
awk '/^server \{$/{c++} c==2' /tmp/hikrad-nginx.conf | sed \
    -e 's/^\tlisten 443 ssl default_server;/\tlisten 80 default_server;/' \
    -e '/listen \[::\]:443/d' \
    -e '/ssl_certificate/d' -e '/ssl_protocols/d' -e '/ssl_prefer_server_ciphers/d' \
    -e '/http2 on/d' \
    > /etc/nginx/http.d/hikrad.conf

# --- launch everything, supervised -------------------------------------------
PIDS=()
run() { "$@" & PIDS+=("$!"); }

log "Starting hikrad-api, hikrad-acct, hikrad-monitor, FreeRADIUS, nginx…"
run su -s /bin/sh hikrad-api -c "HIKRAD_DB_URL='$HIKRAD_DB_URL' HIKRAD_REDIS_URL='$HIKRAD_REDIS_URL' HIKRAD_ENCRYPTION_KEY='$HIKRAD_ENCRYPTION_KEY' HIKRAD_JWT_SECRET='$HIKRAD_JWT_SECRET' HIKRAD_ENV='$HIKRAD_ENV' HIKRAD_MIGRATIONS_DIR='$HIKRAD_MIGRATIONS_DIR' HIKRAD_RADIUS_CLIENTS_PATH='$HIKRAD_RADIUS_CLIENTS_PATH' HIKRAD_BRANDING_DIR='$HIKRAD_BRANDING_DIR' /opt/hikrad/bin/hikrad-api"
run su -s /bin/sh hikrad-acct -c "HIKRAD_DB_URL='$HIKRAD_DB_URL' HIKRAD_REDIS_URL='$HIKRAD_REDIS_URL' HIKRAD_ENV='$HIKRAD_ENV' HIKRAD_ACCT_SPILL_DIR='$HIKRAD_ACCT_SPILL_DIR' /opt/hikrad/bin/hikrad-acct"
run su -s /bin/sh hikrad-monitor -c "HIKRAD_DB_URL='$HIKRAD_DB_URL' HIKRAD_REDIS_URL='$HIKRAD_REDIS_URL' HIKRAD_ENCRYPTION_KEY='$HIKRAD_ENCRYPTION_KEY' HIKRAD_ENV='$HIKRAD_ENV' /opt/hikrad/bin/hikrad-monitor"
sleep 3
run freeradius -f -l stdout -d "$FR_DIR"
run nginx -g "daemon off;"

shutdown() {
    log "shutting down…"
    for pid in "${PIDS[@]}"; do kill "$pid" 2>/dev/null || true; done
    su -s /bin/sh postgres -c "pg_ctl -D '$DATA_DIR/postgres' stop -m fast" 2>/dev/null || true
    exit 0
}
trap shutdown TERM INT

wait -n
log "a supervised process exited — shutting down the container (RouterOS will restart it per its own container policy)."
shutdown
