#!/usr/bin/env bash
# install.sh — HikRAD native installer (v4-2, FR-118). Provisions PostgreSQL
# + TimescaleDB, FreeRADIUS 3 (with zero-restart dynamic NAS clients), nginx,
# and the seven hikrad-* systemd units directly on a Linux host — Docker is
# CANCELLED as the delivery mechanism (Decision 57); nothing here installs
# or depends on it. The RouterOS container target (Decision 58) is a
# completely separate, single-image artifact built by
# deploy/routeros/build.sh — this script never touches it.
#
# Supported: Ubuntu 22.04/24.04/26.04 LTS, Debian 12/13 (tested tier).
# Other systemd Linux is accepted with a warning (Decision 55 stands).
#
# Two modes:
#   Bundle (production — no source tree, no Go/Node toolchain required):
#     sudo ./scripts/install.sh --bundle hikrad-vX.Y.Z.tar
#     Verifies the bundle's signature (scripts/verify-bundle.sh) BEFORE
#     touching anything, then installs its prebuilt binaries + frontend
#     build + FreeRADIUS config straight onto this host.
#   Source (dev-only, needs a full checkout + Go/Node toolchains on THIS
#   host to build first — see docs/ops/dev-setup.md for the lighter-weight
#   `go run`/`npm run dev` inner loop instead of this):
#     sudo ./scripts/install.sh
#
# THE CONTRACT THIS SCRIPT MUST HOLD ON EVERY RE-RUN (gate items 1, 3, 7):
#   - idempotent: re-running upgrades in place — new binaries/config/units
#     are (re)written every run, since that IS how an upgrade is applied —
#     but /etc/hikrad/hikrad.env (secrets) is written ONLY if absent, the
#     chosen nginx port is recorded once and never re-prompted/changed, and
#     directory ownership is (re)applied on EVERY run, fresh install or
#     upgrade alike (anti-pattern A11 — the fix must not only touch the
#     fresh-install path).
#   - non-interactive via env vars: HIKRAD_DB_PASS, HIKRAD_HTTP_PORT,
#     HIKRAD_CF_TOKEN, HIKRAD_VERBOSE, NO_COLOR. Every prompt happens before
#     any change is made — never mid-run.
#
# Test/override knobs (not for production use):
#   HIKRAD_SKIP_OS_CHECK   =1 skips the OS/root check
#   HIKRAD_SKIP_PACKAGES   =1 skips apt-get entirely (assumes postgres/
#                           freeradius/nginx already present — CI/sandboxed
#                           idempotency checks only, mirrors the old
#                           HIKRAD_SKIP_DOCKER knob)
#   HIKRAD_ROOT_OVERRIDE   remaps the frozen /opt,/etc,/var paths under a
#                           prefix for a sandboxed test run (never set in
#                           production — the frozen contract is real paths)
#   --no-start             do everything except starting/restarting units
#   --domain <fqdn>        run certbot for a real TLS cert on this hostname
#   --bundle <path>        install from a signed offline bundle instead of
#                           building from a source checkout

set -euo pipefail
shopt -s nullglob
# `install -d -m MODE dir` only forces MODE on the directories explicitly
# named — any ANCESTOR it has to create implicitly along the way (e.g.
# /opt/hikrad, which is never itself a named target — only /opt/hikrad/bin
# etc. are) gets created under the INVOKING shell's umask instead. Found
# live: `sudo bash install.sh` under a caller with a restrictive personal
# umask (027) silently produced /opt/hikrad at 0750 root:root, which then
# made every hikrad-api/-acct/-monitor unit crash-loop with "Permission
# denied" at exec — systemd's EXEC step needs the whole ancestor chain
# traversable by the unit's own non-root user, and 0750 root:root blocks
# that regardless of the binary's own 0755. A fixed umask makes every
# directory this script creates predictable regardless of who invokes it.
umask 022

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

# --- frozen paths (docs/v4/phases/phase-v4-2-native-install/00-phase.md §4) -
# HIKRAD_ROOT_OVERRIDE exists ONLY so the scripts CI job and local rehearsal
# can point this whole tree at a sandboxed prefix without root — production
# installs never set it, so every path below is the real, frozen one.
PREFIX="${HIKRAD_ROOT_OVERRIDE:-}"
BIN_DIR="$PREFIX/opt/hikrad/bin"
WWW_DIR="$PREFIX/opt/hikrad/www"
MIGRATIONS_DIR="$PREFIX/opt/hikrad/migrations"
VERSION_FILE="$PREFIX/opt/hikrad/VERSION"
RELEASE_ROLLBACK_DIR="$PREFIX/opt/hikrad/release.rollback"
ETC_DIR="$PREFIX/etc/hikrad"
ENV_FILE="$ETC_DIR/hikrad.env"
META_FILE="$ETC_DIR/install.meta"
TLS_DIR="$ETC_DIR/tls"
FR_DIR="$ETC_DIR/freeradius"
VAR_DIR="$PREFIX/var/lib/hikrad"
BACKUP_DIR="$PREFIX/var/backups/hikrad"
SYSTEMD_DIR="$PREFIX/etc/systemd/system"
NGINX_AVAILABLE="$PREFIX/etc/nginx/sites-available"
NGINX_ENABLED="$PREFIX/etc/nginx/sites-enabled"

NO_START=0
DOMAIN=""
BUNDLE=""

while [ $# -gt 0 ]; do
    case "$1" in
        --no-start) NO_START=1; shift ;;
        --domain) DOMAIN="${2:?--domain requires a hostname}"; shift 2 ;;
        --bundle) BUNDLE="${2:?--bundle requires a path to a hikrad-vX.Y.Z.tar file}"; shift 2 ;;
        -h|--help) sed -n '2,45p' "$0"; exit 0 ;;
        *) echo "error: unknown argument '$1' (see --help)" >&2; exit 1 ;;
    esac
done

# --- logging (NO_COLOR / HIKRAD_VERBOSE, frozen env names) ------------------
if [ -t 1 ] && [ "${NO_COLOR:-}" = "" ]; then
    C_BLUE=$'\033[1;34m'; C_YELLOW=$'\033[1;33m'; C_RESET=$'\033[0m'
else
    C_BLUE=""; C_YELLOW=""; C_RESET=""
fi
log()  { printf '%s[hikrad-install]%s %s\n' "$C_BLUE" "$C_RESET" "$*"; }
warn() { printf '%s[hikrad-install] WARNING:%s %s\n' "$C_YELLOW" "$C_RESET" "$*" >&2; }
die()  { printf '[hikrad-install] ERROR: %s\n' "$*" >&2; exit 1; }
vlog() { [ "${HIKRAD_VERBOSE:-0}" = "1" ] && printf '[hikrad-install][v] %s\n' "$*" || true; }

# --- 1. OS + privilege check -------------------------------------------------
if [ "${HIKRAD_SKIP_OS_CHECK:-0}" != "1" ]; then
    [ "$(id -u)" -eq 0 ] || die "must run as root (sudo ./scripts/install.sh)"
    [ -r /etc/os-release ] || die "cannot read /etc/os-release — not a systemd-standard Linux; set HIKRAD_SKIP_OS_CHECK=1 to install anyway."
    # shellcheck disable=SC1091
    . /etc/os-release
    case "${ID:-}:${VERSION_ID:-}" in
        ubuntu:22.04|ubuntu:24.04|ubuntu:26.04) log "OS check passed: $PRETTY_NAME" ;;
        debian:12|debian:13) log "OS check passed: $PRETTY_NAME" ;;
        *) warn "$PRETTY_NAME is not the QA'd tier (Ubuntu 22.04/24.04/26.04 LTS, Debian 12/13) — continuing anyway (Decision 55). Set HIKRAD_SKIP_OS_CHECK=1 to skip this check entirely." ;;
    esac
    command -v systemctl >/dev/null 2>&1 || die "systemd (systemctl) not found — the native installer requires it. Set HIKRAD_SKIP_OS_CHECK=1 only for a sandboxed --no-start rehearsal with no real service management."
fi

# --- 2. Resolve the HTTP port BEFORE any work begins (frozen: every prompt --
# happens up front, never mid-run — non-interactive via HIKRAD_HTTP_PORT).
# Once chosen, it is recorded in install.meta and NEVER re-prompted or
# changed by a later re-run (gate items 3 and 5).
#
# HIKRAD_INTERNAL_API_PORT: hikrad-api always binds 127.0.0.1:8080
# (cmd/hikrad-api/main.go) — fixed, not configurable, since nginx/
# FreeRADIUS/hikrad-updaterd all assume it. The PUBLIC nginx port must never
# be the same number: found live (v4-2's first real install run) — 8080 is
# also the single most common "port 80 is taken, try this instead" instinct
# a human or a script reaches for, and it used to be this script's own
# suggested default, so an operator following the obvious prompt landed
# nginx and hikrad-api on the same port and hikrad-api crash-looped forever
# on "bind: address already in use" the moment nginx (which starts first,
# via its own apt postinst) claimed 8080.
readonly HIKRAD_INTERNAL_API_PORT=8080

port_in_use() { # port_in_use PORT -> 0 if something is already listening
    command -v ss >/dev/null 2>&1 && ss -ltn "( sport = :$1 )" 2>/dev/null | grep -q ":$1" && return 0
    command -v ss >/dev/null 2>&1 || return 1
    return 1
}

resolve_http_port() {
    local existing=""
    if [ -f "$META_FILE" ]; then
        # shellcheck disable=SC1090
        existing="$(. "$META_FILE" 2>/dev/null; echo "${HIKRAD_HTTP_PORT:-}")"
    fi
    if [ -n "$existing" ]; then
        HTTP_PORT="$existing"
        log "Re-using previously chosen HTTP port: $HTTP_PORT (never changed by a re-run)"
        if [ "$HTTP_PORT" = "$HIKRAD_INTERNAL_API_PORT" ]; then
            die "install.meta has HIKRAD_HTTP_PORT=$HIKRAD_INTERNAL_API_PORT, which collides with hikrad-api's own fixed internal port — this install predates the fix for that collision. Edit $META_FILE, change HIKRAD_HTTP_PORT to something else, then re-run."
        fi
        return
    fi
    if [ -n "${HIKRAD_HTTP_PORT:-}" ]; then
        HTTP_PORT="$HIKRAD_HTTP_PORT"
        log "Using HIKRAD_HTTP_PORT=$HTTP_PORT (non-interactive)"
        if [ "$HTTP_PORT" = "$HIKRAD_INTERNAL_API_PORT" ]; then
            die "HIKRAD_HTTP_PORT=$HIKRAD_INTERNAL_API_PORT collides with hikrad-api's own fixed internal port ($HIKRAD_INTERNAL_API_PORT) — choose a different port."
        fi
        return
    fi
    HTTP_PORT=80
    if port_in_use 80; then
        if [ -t 0 ] && [ -t 1 ]; then
            warn "port 80 is already in use by something else on this host."
            while :; do
                read -r -p "Enter an alternative HTTP port to use instead [8880]: " chosen
                HTTP_PORT="${chosen:-8880}"
                if [ "$HTTP_PORT" = "$HIKRAD_INTERNAL_API_PORT" ]; then
                    warn "$HIKRAD_INTERNAL_API_PORT is hikrad-api's own fixed internal port — choose a different number."
                    continue
                fi
                break
            done
        else
            die "port 80 is in use and this is a non-interactive run — set HIKRAD_HTTP_PORT=<port> (not $HIKRAD_INTERNAL_API_PORT) and re-run."
        fi
    fi
}
resolve_http_port
HTTPS_PORT=$((HTTP_PORT == 80 ? 443 : HTTP_PORT + 1))
if [ -f "$META_FILE" ]; then
    # shellcheck disable=SC1090
    existing_https="$(. "$META_FILE" 2>/dev/null; echo "${HIKRAD_HTTPS_PORT:-}")"
    if [ -n "${existing_https:-}" ]; then HTTPS_PORT="$existing_https"; fi
fi
[ "$HTTPS_PORT" != "$HIKRAD_INTERNAL_API_PORT" ] \
    || die "the derived HTTPS port ($HTTPS_PORT, one above HIKRAD_HTTP_PORT=$HTTP_PORT) collides with hikrad-api's own fixed internal port — choose a different HIKRAD_HTTP_PORT."

# Whether nginx was already on this host BEFORE this run — decides, in step
# 9, whether the distro's stock default site may be disabled (only if WE just
# installed nginx, so nobody can be relying on it yet).
NGINX_PREEXISTING=0
command -v nginx >/dev/null 2>&1 && NGINX_PREEXISTING=1

# --- 3. OS package dependencies (idempotent — apt-get install is already a --
# no-op on an already-satisfied package; TimescaleDB/PGDG repo files are only
# added if missing).
if [ "${HIKRAD_SKIP_PACKAGES:-0}" != "1" ]; then
    command -v apt-get >/dev/null 2>&1 || die "apt-get not found — this installer currently supports Debian/Ubuntu package management only (Decision 55's 'warn, never silently half-install' — file an issue for other distros)."
    export DEBIAN_FRONTEND=noninteractive

    # shellcheck disable=SC1091
    . /etc/os-release
    log "Installing OS package dependencies…"
    apt-get update -qq
    # The repo-key steps below pipe curl into gpg — both must exist BEFORE
    # the main install further down. Minimal Ubuntu/Debian server images ship
    # neither gnupg nor (sometimes) curl, and the install died right here on
    # "gpg: command not found" (found in a clean ubuntu:24.04 rehearsal).
    apt-get install -y -qq curl gnupg ca-certificates lsb-release \
        || die "could not install the bootstrap packages (curl, gnupg, ca-certificates)"

    # PostgreSQL + TimescaleDB: add the upstream repos only if not already
    # present (idempotent — a second run must not duplicate apt sources).
    if [ ! -f /etc/apt/sources.list.d/pgdg.list ]; then
        install -d -m 0755 /usr/share/keyrings
        curl -fsSL https://www.postgresql.org/media/keys/ACCC4CF8.asc \
            | gpg --dearmor -o /usr/share/keyrings/postgresql.gpg
        # gpg --dearmor writes 0600 root:root by default — apt's sandboxed
        # _apt user (Debian trixie/modern Ubuntu privilege-separate the
        # actual fetch+verify step) can't read that, and the repo add
        # fails with a confusing "not signed" error that looks like a
        # signature problem, not a permissions one (found live: v4-2's
        # first real install run on Debian 13).
        chmod 0644 /usr/share/keyrings/postgresql.gpg
        echo "deb [signed-by=/usr/share/keyrings/postgresql.gpg] https://apt.postgresql.org/pub/repos/apt ${VERSION_CODENAME}-pgdg main" \
            > /etc/apt/sources.list.d/pgdg.list
    fi
    if [ ! -f /etc/apt/sources.list.d/timescaledb.list ]; then
        install -d -m 0755 /usr/share/keyrings
        curl -fsSL https://packagecloud.io/timescale/timescaledb/gpgkey \
            | gpg --dearmor -o /usr/share/keyrings/timescaledb.gpg
        chmod 0644 /usr/share/keyrings/timescaledb.gpg
        echo "deb [signed-by=/usr/share/keyrings/timescaledb.gpg] https://packagecloud.io/timescale/timescaledb/${ID}/ ${VERSION_CODENAME} main" \
            > /etc/apt/sources.list.d/timescaledb.list
    fi
    apt-get update -qq

    apt-get install -y -qq \
        postgresql postgresql-contrib \
        freeradius freeradius-utils \
        nginx \
        wireguard wireguard-tools \
        openssl gnupg curl ca-certificates \
        redis-server \
        cron \
        || die "package installation failed — see apt-get's own output above"

    # TimescaleDB's package name is versioned to the installed PG major
    # version (e.g. timescaledb-2-postgresql-16); resolve it rather than
    # hardcoding one PG version across every supported OS release.
    pg_major="$(pg_config --version 2>/dev/null | grep -oE '[0-9]+' | head -1)"
    [ -n "$pg_major" ] || pg_major="$(psql -V 2>/dev/null | grep -oE '[0-9]+' | head -1)"
    if [ -n "$pg_major" ]; then
        if apt-get install -y -qq "timescaledb-2-postgresql-${pg_major}"; then
            # The package alone is not enough — TimescaleDB is a shared-
            # preload extension; CREATE EXTENSION fails with "must be
            # preloaded" until shared_preload_libraries names it AND
            # postgres has been restarted to actually load it (found live:
            # v4-2's first real install run — the package installed clean,
            # the extension still failed). timescaledb-tune is the
            # package's own recommended, idempotent way to do this.
            if command -v timescaledb-tune >/dev/null 2>&1; then
                timescaledb-tune --quiet --yes >/dev/null 2>&1 \
                    || warn "timescaledb-tune failed — add 'timescaledb' to shared_preload_libraries in postgresql.conf manually, then restart postgresql."
                systemctl restart postgresql \
                    || warn "postgresql restart after timescaledb-tune failed — CREATE EXTENSION timescaledb will keep failing until it's restarted."
                for i in $(seq 1 15); do
                    pg_isready >/dev/null 2>&1 && break
                    sleep 1
                done
            else
                warn "timescaledb-tune not found — add 'timescaledb' to shared_preload_libraries in postgresql.conf manually, then restart postgresql."
            fi
        else
            warn "timescaledb-2-postgresql-${pg_major} failed to install — TimescaleDB features will be unavailable until this is resolved manually."
        fi
    else
        warn "could not detect the installed PostgreSQL major version — skipping the TimescaleDB package; install timescaledb-2-postgresql-<N> manually."
    fi

    # cloudflared: installed but inert until `hikrad tunnel enable` (FR-118
    # §3.1 step 8 — "installed but inert until configured").
    if ! command -v cloudflared >/dev/null 2>&1; then
        arch="$(dpkg --print-architecture)"
        curl -fsSL "https://github.com/cloudflare/cloudflared/releases/latest/download/cloudflared-linux-${arch}" \
            -o /usr/local/bin/cloudflared \
            && chmod 0755 /usr/local/bin/cloudflared \
            || warn "cloudflared download failed — Settings > Remote Access will be unavailable until it's installed manually."
    fi

    systemctl enable --now postgresql >/dev/null 2>&1 || true
    systemctl enable --now redis-server >/dev/null 2>&1 || true

    # The distro's OWN freeradius.service must never run — hikrad-radius-
    # worker.service (below) is a second, HikRAD-owned instance of the same
    # binary pointed at /etc/hikrad/freeradius; running both would fight over
    # UDP 1812/1813/3799.
    systemctl disable --now freeradius >/dev/null 2>&1 || true
else
    log "HIKRAD_SKIP_PACKAGES=1: skipping apt-get (assuming postgres/freeradius/nginx/redis are already present)"
fi

# --- 4. PostgreSQL: detect existing role/db, create idempotently, refuse ----
# with a stated reason rather than silently reusing a wrong instance.
if [ "${HIKRAD_SKIP_PACKAGES:-0}" != "1" ]; then
    command -v psql >/dev/null 2>&1 || die "psql not found after package install — PostgreSQL provisioning cannot continue."
    systemctl is-active --quiet postgresql \
        || die "PostgreSQL service is not active after install — check 'systemctl status postgresql' and re-run."

    PG_PASSWORD="${HIKRAD_DB_PASS:-}"
    if [ -f "$ENV_FILE" ]; then
        PG_PASSWORD="$(grep '^POSTGRES_PASSWORD=' "$ENV_FILE" 2>/dev/null | cut -d= -f2-)"
    fi
    [ -n "$PG_PASSWORD" ] || PG_PASSWORD="$(openssl rand -hex 24)"

    role_exists="$(sudo -u postgres psql -tAc "SELECT 1 FROM pg_roles WHERE rolname='hikrad'" 2>/dev/null || true)"
    if [ "$role_exists" = "1" ] && [ -f "$ENV_FILE" ]; then
        vlog "role 'hikrad' already exists — leaving its password untouched (never re-set on an existing role, matches the 'never regenerate secrets' contract)"
    elif [ "$role_exists" = "1" ]; then
        # The role survived but hikrad.env did not (deleted /etc/hikrad, a
        # half-finished uninstall, a reinstall onto a kept database). Step 6
        # is about to write a brand-new hikrad.env with $PG_PASSWORD, so the
        # role MUST be re-keyed to match — leaving it untouched guaranteed
        # hikrad-api could never log in ("password authentication failed").
        # There is no old secret left to preserve: it lived only in the
        # hikrad.env that is already gone.
        warn "PostgreSQL role 'hikrad' exists but $ENV_FILE does not — re-keying the role to the password being written to the new hikrad.env."
        sudo -u postgres psql -v ON_ERROR_STOP=1 -c \
            "ALTER ROLE hikrad LOGIN PASSWORD '$(printf '%s' "$PG_PASSWORD" | sed "s/'/''/g")';" \
            || die "failed to re-key the existing 'hikrad' PostgreSQL role"
        db_has_data="$(sudo -u postgres psql -d hikrad -tAc "SELECT 1 FROM information_schema.tables WHERE table_name='subscribers'" 2>/dev/null || true)"
        if [ "$db_has_data" = "1" ]; then
            warn "the existing 'hikrad' database already holds data, but a NEW HIKRAD_ENCRYPTION_KEY is about to be generated — subscriber RADIUS passwords and NAS secrets encrypted under the old key will NOT decrypt. Restore the old hikrad.env (or a 'hikrad restore' backup + its passphrase) instead if you still have it."
        fi
    else
        sudo -u postgres psql -v ON_ERROR_STOP=1 -c \
            "CREATE ROLE hikrad LOGIN PASSWORD '$(printf '%s' "$PG_PASSWORD" | sed "s/'/''/g")';" \
            || die "failed to create the 'hikrad' PostgreSQL role"
    fi

    db_exists="$(sudo -u postgres psql -tAc "SELECT 1 FROM pg_database WHERE datname='hikrad'" 2>/dev/null || true)"
    if [ "$db_exists" != "1" ]; then
        sudo -u postgres psql -v ON_ERROR_STOP=1 -c "CREATE DATABASE hikrad OWNER hikrad;" \
            || die "failed to create the 'hikrad' database"
    fi
    sudo -u postgres psql -v ON_ERROR_STOP=1 -d hikrad -c "CREATE EXTENSION IF NOT EXISTS timescaledb;" \
        || warn "CREATE EXTENSION timescaledb failed — install the timescaledb-2-postgresql-<N> package and run this again; hikrad-api's own migrations will otherwise fail."
fi

# --- 5. Directories + system users + ownership ------------------------------
# Runs on EVERY invocation, fresh install AND upgrade alike (anti-pattern
# A11 — this is the fix, and it is not conditional on "first run").
log "Creating directories and system users (idempotent, also runs on upgrade)…"

ensure_user() { # ensure_user NAME
    id -u "$1" >/dev/null 2>&1 || useradd --system --no-create-home --shell /usr/sbin/nologin "$1"
}
ensure_user hikrad-api
ensure_user hikrad-acct
ensure_user hikrad-monitor
ensure_user hikrad-notification

install -d -m 0755 "$PREFIX/opt/hikrad"
install -d -m 0755 "$BIN_DIR" "$WWW_DIR" "$MIGRATIONS_DIR"
# 0755, not 0750: a service running as its own non-root user (hikrad-api,
# freerad, ...) needs traverse (x) permission on EVERY ancestor directory
# to reach its own, more-restrictively-owned subdirectory below — a 0750
# root:root parent blocks that regardless of the leaf's own ownership.
# Found live: hikrad-radius-worker's freerad user couldn't read its own
# config at all ("Permission denied" on a file it owns, during `freeradius
# -CX`) because /etc/hikrad itself was 0750. Nothing actually secret lives
# directly in these two directories — hikrad.env (0600) and tls/ (0700)
# protect themselves independently at the file/leaf level.
install -d -m 0755 "$ETC_DIR"
install -d -m 0700 "$TLS_DIR"
install -d -m 0755 "$FR_DIR"
install -d -m 0750 "$BACKUP_DIR"
install -d -m 0755 "$VAR_DIR"

install -d -m 0750 -o hikrad-api -g hikrad-api "$VAR_DIR/branding"
install -d -m 0750 -o hikrad-acct -g hikrad-acct "$VAR_DIR/acct-spill"
# 0777: shared IPC between hikrad-api (uid varies) and the freeradius worker
# (freerad user, package-managed) — no secret lives in this directory, the
# control socket's own peercred/mode is the real boundary, same reasoning
# install.sh has always used for this one directory.
install -d -m 0777 "$VAR_DIR/radius-control"
install -d -m 0750 "$VAR_DIR/incoming"
install -d -m 0750 "$VAR_DIR/data/updater"
install -d -m 0755 "$PREFIX/var/log/hikrad-radius"
if getent passwd freerad >/dev/null 2>&1; then
    chown freerad:freerad "$PREFIX/var/log/hikrad-radius" 2>/dev/null || true
fi

log "Directories and ownership reconciled."

# --- 6. hikrad.env: write ONLY if absent — this is the "never regenerate ---
# existing secrets" guarantee (gate item 3).
if [ ! -f "$ENV_FILE" ]; then
    log "Generating $ENV_FILE (fresh secrets)…"
    HIKRAD_DB_PASS="${PG_PASSWORD:-${HIKRAD_DB_PASS:-}}" \
        bash "$SCRIPT_DIR/gen-env.sh" --env prod "$ENV_FILE"
    chmod 0600 "$ENV_FILE"
else
    log "$ENV_FILE already exists — leaving all secrets untouched."
fi

# --- 7. Stage the release: bundle (verified) or local source checkout ------
HIKRAD_DELIVERY_MODE="source"
STAGE_BIN="$REPO_DIR/backend" # source-mode fallback locations
STAGE_WWW_PANEL="$REPO_DIR/frontend/panel/dist"
STAGE_WWW_PORTAL="$REPO_DIR/frontend/portal/dist"
STAGE_FR="$REPO_DIR/deploy/freeradius"
STAGE_MIGRATIONS="$REPO_DIR/backend/migrations"
STAGE_SYSTEMD="$REPO_DIR/deploy/systemd"
STAGE_NGINX_TEMPLATE="$REPO_DIR/deploy/nginx/hikrad.conf.template"
STAGE_CLI="$SCRIPT_DIR/hikrad"
STAGE_VERSION=""

if [ -n "$BUNDLE" ]; then
    [ -f "$BUNDLE" ] || die "--bundle file not found: $BUNDLE"
    log "Verifying release bundle $BUNDLE before using anything from it…"
    SCRATCH="$(mktemp -d)"
    trap 'rm -rf "$SCRATCH"' EXIT
    tar -xf "$BUNDLE" -C "$SCRATCH"
    bash "$SCRATCH/scripts/verify-bundle.sh" "$SCRATCH" \
        || die "bundle verification failed — refusing to install from $BUNDLE (nothing was changed)"
    log "Bundle verified."

    STAGE_BIN=""             # bundle ships prebuilt binaries directly, no go build
    STAGE_WWW_PANEL="$SCRATCH/www/panel"
    STAGE_WWW_PORTAL="$SCRATCH/www/portal"
    STAGE_FR="$SCRATCH/freeradius"
    STAGE_MIGRATIONS="$SCRATCH/migrations"
    STAGE_SYSTEMD="$SCRATCH/systemd"
    STAGE_NGINX_TEMPLATE="$SCRATCH/nginx/hikrad.conf.template"
    STAGE_CLI="$SCRATCH/scripts/hikrad"
    STAGE_VERSION="$SCRATCH/VERSION"
    BUNDLE_BIN_DIR="$SCRATCH/bin"
    HIKRAD_DELIVERY_MODE="bundle"
else
    # sudo's secure_path drops /usr/local/go/bin (the go.dev tarball's
    # documented location), so a Go the invoking user can run is invisible
    # here under plain `sudo ./scripts/install.sh`.
    if ! command -v go >/dev/null 2>&1 && [ -x /usr/local/go/bin/go ]; then
        PATH="$PATH:/usr/local/go/bin"
    fi
    command -v go >/dev/null 2>&1 || die "no --bundle given and no Go toolchain found — source-mode install needs Go to build the binaries. Either install Go, or use --bundle with a signed release (see docs/ops/install-guide.md)."
    log "Source mode: building binaries from $REPO_DIR/backend…"
    ( cd "$REPO_DIR/backend" && for svc in hikrad-api hikrad-acct hikrad-monitor hikrad-updaterd; do
        CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o "/tmp/hikrad-build-$svc" "./cmd/$svc"
      done ) || die "source build failed"
    BUNDLE_BIN_DIR="/tmp/hikrad-build-dir-does-not-exist" # unused in source mode, see copy step below
fi

# --- 7a. Backup current bin/www/freeradius for rollback (upgrade path only) -
# Same one-deep rollback pattern scripts/hikrad's cmd_update has always used
# for bundle-mode's release directory — generalized here to cover the binary
# tree too, since there is no image id to re-tag without Docker (FR-121).
if [ -d "$BIN_DIR" ] && [ -n "$(ls -A "$BIN_DIR" 2>/dev/null || true)" ]; then
    rm -rf "$RELEASE_ROLLBACK_DIR"
    install -d -m 0755 "$RELEASE_ROLLBACK_DIR"
    cp -a "$BIN_DIR" "$RELEASE_ROLLBACK_DIR/bin" 2>/dev/null || true
    [ -d "$WWW_DIR" ] && cp -a "$WWW_DIR" "$RELEASE_ROLLBACK_DIR/www" 2>/dev/null || true
    [ -d "$FR_DIR" ] && cp -a "$FR_DIR" "$RELEASE_ROLLBACK_DIR/freeradius" 2>/dev/null || true
fi

# --- 7b. Install binaries -----------------------------------------------------
if [ -n "$BUNDLE" ]; then
    for svc in hikrad-api hikrad-acct hikrad-monitor hikrad-updaterd hikrad-notification; do
        if [ -f "$BUNDLE_BIN_DIR/$svc" ]; then
            install -m 0755 "$BUNDLE_BIN_DIR/$svc" "$BIN_DIR/$svc"
        fi
    done
else
    for svc in hikrad-api hikrad-acct hikrad-monitor hikrad-updaterd; do
        if [ -f "/tmp/hikrad-build-$svc" ]; then
            install -m 0755 "/tmp/hikrad-build-$svc" "$BIN_DIR/$svc"
            rm -f "/tmp/hikrad-build-$svc"
        fi
    done
fi
log "Installed binaries to $BIN_DIR."

# --- 7c. Frontend (built dists; a bundle ships them prebuilt, source mode --
# builds them here if npm is available and dist/ doesn't already exist).
if [ ! -d "$STAGE_WWW_PANEL" ] || [ ! -d "$STAGE_WWW_PORTAL" ]; then
    if [ -z "$BUNDLE" ] && command -v npm >/dev/null 2>&1; then
        log "Building frontend (panel + portal)…"
        ( cd "$REPO_DIR/frontend" && npm ci --silent && npm run build --workspaces --if-present --silent ) \
            || warn "frontend build failed — panel/portal will 404 until 'cd frontend && npm run build --workspaces' succeeds."
    fi
fi
if [ -d "$STAGE_WWW_PANEL" ]; then rm -rf "$WWW_DIR/panel"; cp -a "$STAGE_WWW_PANEL" "$WWW_DIR/panel"; fi
if [ -d "$STAGE_WWW_PORTAL" ]; then rm -rf "$WWW_DIR/portal"; cp -a "$STAGE_WWW_PORTAL" "$WWW_DIR/portal"; fi

# --- 7d. Migrations (hikrad-api applies them on boot; HIKRAD_MIGRATIONS_DIR -
# in the systemd unit points here).
if [ -d "$STAGE_MIGRATIONS" ]; then
    rm -rf "$MIGRATIONS_DIR"
    install -d -m 0755 "$MIGRATIONS_DIR"
    cp "$STAGE_MIGRATIONS"/*.up.sql "$MIGRATIONS_DIR/" 2>/dev/null || true
fi

# --- 7e. VERSION file (read by hikrad-updaterd + the panel's Settings > ----
# System screen).
if [ -n "$STAGE_VERSION" ] && [ -f "$STAGE_VERSION" ]; then
    cp "$STAGE_VERSION" "$VERSION_FILE"
elif [ ! -f "$VERSION_FILE" ]; then
    echo "dev" > "$VERSION_FILE"
fi

# --- 8. FreeRADIUS: copy config, retarget paths, provision the discovery ---
# client, validate BEFORE enabling the unit (gate item 8).
log "Staging FreeRADIUS config to $FR_DIR…"
rm -rf "$FR_DIR"
cp -a "$STAGE_FR" "$FR_DIR"

# Retarget the three hardcoded paths radiusd.conf's stock values assume
# (/etc/freeradius, /var/log/freeradius, /var/run/<name>) onto HikRAD's own
# tree — applied fresh every run against the pristine copy above, so this is
# naturally idempotent and never drifts from source.
sed -i \
    -e "s#^raddbdir = /etc/freeradius#raddbdir = $FR_DIR#" \
    -e "s#^logdir = /var/log/freeradius#logdir = $PREFIX/var/log/hikrad-radius#" \
    -e "s#^run_dir = \${localstatedir}/run/\${name}#run_dir = $VAR_DIR/radius-control#" \
    "$FR_DIR/radiusd.conf"

# clients-generated.conf is written by hikrad-api (a different user) —
# same fix shape as the old acct-spill/branding chown bugs (anti-pattern
# A11): create it with the right owner BEFORE hikrad-api ever needs to
# write it, on fresh install AND every upgrade.
touch "$FR_DIR/clients-generated.conf"
chown hikrad-api:hikrad-api "$FR_DIR/clients-generated.conf" 2>/dev/null || true
chmod 0640 "$FR_DIR/clients-generated.conf"

if command -v freeradius >/dev/null 2>&1; then
    # --- 8a. FreeRADIUS EAP certs: generate once per install, never checked
    # in (FR-127) — a known, identical-across-every-clone private key on an
    # EAP surface is indefensible, and that is exactly what the repo shipped
    # until this phase deleted the checked-in certs/*.key, *.p12 files.
    # $FR_DIR is wiped and freshly re-copied from $STAGE_FR on EVERY run
    # (fresh install and every upgrade alike, see step 8 above), so certs
    # must live in a location that wipe never touches — same "generate once,
    # survive every re-run" shape as $TLS_DIR below, applied here so an
    # upgrade never silently invalidates an already-enrolled EAP client's
    # trust of the previous CA.
    FR_CERTS_STABLE="$VAR_DIR/freeradius-certs"
    if command -v openssl >/dev/null 2>&1; then
        if [ ! -f "$FR_CERTS_STABLE/server.key" ]; then
            log "Generating unique FreeRADIUS EAP certificates for this install…"
            install -d -m 0750 "$FR_CERTS_STABLE"
            ( cd "$FR_DIR/certs" && ./bootstrap ) >/tmp/hikrad-freeradius-certgen.log 2>&1 \
                || die "FreeRADIUS cert generation (certs/bootstrap) failed — see /tmp/hikrad-freeradius-certgen.log"
            cp "$FR_DIR"/certs/ca.key "$FR_DIR"/certs/ca.pem "$FR_DIR"/certs/ca.der \
               "$FR_DIR"/certs/ca.crl "$FR_DIR"/certs/server.key "$FR_DIR"/certs/server.pem \
               "$FR_DIR"/certs/server.crt "$FR_DIR"/certs/client.key "$FR_DIR"/certs/client.pem \
               "$FR_DIR"/certs/client.crt "$FR_DIR"/certs/client.p12 "$FR_DIR"/certs/server.p12 \
               "$FR_DIR"/certs/dh "$FR_DIR"/certs/index.txt "$FR_DIR"/certs/index.txt.attr \
               "$FR_DIR"/certs/serial "$FR_DIR"/certs/passwords.mk \
               "$FR_CERTS_STABLE/" 2>/dev/null || true
        else
            vlog "FreeRADIUS EAP certs already present at $FR_CERTS_STABLE — restoring into the freshly-staged config tree, not regenerating."
            cp "$FR_CERTS_STABLE"/* "$FR_DIR/certs/" 2>/dev/null || true
        fi
        chown -R freerad:freerad "$FR_DIR/certs" "$FR_CERTS_STABLE" 2>/dev/null || true
        chmod 0640 "$FR_DIR"/certs/*.key "$FR_CERTS_STABLE"/*.key 2>/dev/null || true
    else
        warn "openssl not found — skipping FreeRADIUS EAP cert generation; EAP methods will not work until certs exist under $FR_CERTS_STABLE."
    fi

    log "Validating FreeRADIUS config before enabling the unit (gate item 8)…"
    if ! freeradius -CX -d "$FR_DIR" >/tmp/hikrad-freeradius-check.log 2>&1; then
        cat /tmp/hikrad-freeradius-check.log >&2
        die "FreeRADIUS config failed validation (freeradius -CX) — refusing to enable hikrad-radius-worker. See the output above."
    fi
    log "FreeRADIUS config validated OK."
else
    warn "freeradius binary not found — skipping config validation (expected only with HIKRAD_SKIP_PACKAGES=1, a test-only knob; a real install always has it from step 3)."
fi

# --- 9. nginx: additive site, port-fallback aware (gate items 4, 5) --------
log "Configuring nginx (additive — no other site is touched)…"
install -d -m 0755 "$NGINX_AVAILABLE" "$NGINX_ENABLED"
sed -e "s/@@HTTP_PORT@@/$HTTP_PORT/g" -e "s/@@HTTPS_PORT@@/$HTTPS_PORT/g" \
    "$STAGE_NGINX_TEMPLATE" > "$NGINX_AVAILABLE/hikrad.conf"
# The template's standalone `http2 on;` directive exists only from nginx
# 1.25.1. The tested tier ships older nginx (Ubuntu 22.04: 1.18, Ubuntu
# 24.04: 1.24, Debian 12: 1.22), where `nginx -t` rejects it as an unknown
# directive and the install aborts. On those, use the older
# `listen … ssl http2` form instead, which they all accept.
if command -v nginx >/dev/null 2>&1; then
    nginx_ver="$(nginx -v 2>&1 | grep -oE '[0-9]+\.[0-9]+\.[0-9]+' | head -1)"
    if [ -n "$nginx_ver" ] && [ "$(printf '%s\n%s\n' "$nginx_ver" 1.25.1 | sort -V | head -1)" != "1.25.1" ]; then
        vlog "nginx $nginx_ver predates 'http2 on;' — using 'listen … ssl http2' instead"
        sed -i -e '/^[[:space:]]*http2 on;[[:space:]]*$/d' \
               -e 's/^\([[:space:]]*listen[[:space:]].*\) ssl\( default_server\)\{0,1\};/\1 ssl http2\2;/' \
               "$NGINX_AVAILABLE/hikrad.conf"
    fi
fi
ln -sf "$NGINX_AVAILABLE/hikrad.conf" "$NGINX_ENABLED/hikrad.conf"

# nginx allows ONE default_server per listen port. Stock Ubuntu/Debian nginx
# enables sites-enabled/default with `listen 80 default_server`, so on a
# fresh server `nginx -t` failed ("duplicate default server") and the install
# aborted (found in a clean ubuntu:24.04 rehearsal).
#   - nginx installed by THIS run: its stock placeholder site serves nobody
#     yet — disable its symlink (the file stays in sites-available), so the
#     panel answers on http://<server-ip>/.
#   - nginx pre-existed: another site deliberately owns the default server;
#     never touch it (gate item 4) — HikRAD drops default_server instead.
other_default=""
for site in "$NGINX_ENABLED"/*; do
    [ "$(basename "$site")" = "hikrad.conf" ] && continue
    if grep -qE "^[[:space:]]*listen[[:space:]]+(\[::\]:)?($HTTP_PORT|$HTTPS_PORT)[[:space:]].*default_server" "$site" 2>/dev/null; then
        other_default="$site"
        break
    fi
done
if [ -n "$other_default" ]; then
    if [ "$NGINX_PREEXISTING" = "0" ] && [ "$(basename "$other_default")" = "default" ]; then
        log "Disabling nginx's stock placeholder site (installed by this run; the file stays in $NGINX_AVAILABLE/default)."
        rm -f "$other_default"
    else
        warn "$other_default already owns nginx's default server on port $HTTP_PORT/$HTTPS_PORT — leaving it untouched; HikRAD's site will answer only when no other server_name matches."
        sed -i 's/ default_server;/;/' "$NGINX_AVAILABLE/hikrad.conf"
    fi
fi

if [ ! -f "$TLS_DIR/cert.pem" ]; then
    log "Generating a self-signed TLS certificate covering this server's detected address(es)…"
    SAN="DNS:localhost,IP:127.0.0.1"
    for ip in $(hostname -I 2>/dev/null); do SAN="$SAN,IP:$ip"; done
    openssl req -x509 -nodes -newkey rsa:2048 -days 3650 \
        -keyout "$TLS_DIR/key.pem" -out "$TLS_DIR/cert.pem" \
        -subj "/CN=hikrad" -addext "subjectAltName=$SAN" >/dev/null 2>&1
    chmod 644 "$TLS_DIR/cert.pem"; chmod 600 "$TLS_DIR/key.pem"
    log "Self-signed cert covers: $SAN"
else
    vlog "TLS cert already present at $TLS_DIR — left untouched."
fi

if command -v nginx >/dev/null 2>&1; then
    nginx -t >/tmp/hikrad-nginx-check.log 2>&1 || { cat /tmp/hikrad-nginx-check.log >&2; die "nginx config failed validation (nginx -t)."; }
fi

if [ -n "$DOMAIN" ]; then
    log "Requesting a real TLS certificate for $DOMAIN via certbot…"
    apt-get install -y -qq python3-certbot-nginx >/dev/null 2>&1 || true
    if command -v certbot >/dev/null 2>&1; then
        certbot --nginx -d "$DOMAIN" --non-interactive --agree-tos -m "admin@$DOMAIN" --redirect \
            || warn "certbot failed — the self-signed default remains in place. Point $DOMAIN's DNS at this server and re-run with --domain."
    else
        warn "certbot not available — install python3-certbot-nginx and re-run with --domain."
    fi
fi

# --- 10. systemd units --------------------------------------------------------
log "Installing systemd units…"
install -d -m 0755 "$SYSTEMD_DIR"
unit_count=0
for unit in "$STAGE_SYSTEMD"/*.service; do
    install -m 0644 "$unit" "$SYSTEMD_DIR/$(basename "$unit")"
    unit_count=$((unit_count + 1))
done
[ "$unit_count" -ge 1 ] || die "no systemd unit files found under $STAGE_SYSTEMD — release layout looks wrong, refusing to continue"
systemctl daemon-reload

ENABLE_LIST="hikrad-api hikrad-acct hikrad-monitor hikrad-radius-worker"
if [ -x "$BIN_DIR/hikrad-updaterd" ]; then
    ENABLE_LIST="$ENABLE_LIST hikrad-updaterd"
fi
if [ -x "$BIN_DIR/hikrad-notification" ]; then
    ENABLE_LIST="$ENABLE_LIST hikrad-notification"
else
    vlog "hikrad-notification binary not present yet (ships in v4-10) — unit installed but not enabled."
fi

if [ "$NO_START" -eq 1 ]; then
    log "--no-start given: units installed but not enabled/started."
else
    for unit in $ENABLE_LIST; do
        systemctl enable "$unit" >/dev/null 2>&1 || true
        systemctl restart "$unit" \
            || warn "$unit failed to (re)start — check: systemctl status $unit; journalctl -u $unit"
    done
    systemctl reload nginx 2>/dev/null || systemctl restart nginx
fi

# --- 10b. Cloudflare tunnel bootstrap (HIKRAD_CF_TOKEN, frozen non---------
# interactive env var). Installed-but-inert by default (FR-118 §3.1 step 8);
# only started here if a token was actually given — post-install management
# stays through the panel's Settings > Remote Access + `hikrad tunnel
# enable|disable` (DB-driven, see scripts/hikrad), this is purely a
# fully-non-interactive-install convenience.
if [ -n "${HIKRAD_CF_TOKEN:-}" ]; then
    printf '%s' "$HIKRAD_CF_TOKEN" > "$ETC_DIR/cloudflared-token"
    chmod 0600 "$ETC_DIR/cloudflared-token"
    if [ "$NO_START" -ne 1 ]; then
        systemctl enable --now hikrad-tunnel >/dev/null 2>&1 \
            || warn "hikrad-tunnel failed to start — check: systemctl status hikrad-tunnel"
    fi
fi

# --- 11. nightly backup timer (native replacement for the old cron entry) --
BACKUP_TIMER_SERVICE="$SYSTEMD_DIR/hikrad-backup.service"
BACKUP_TIMER="$SYSTEMD_DIR/hikrad-backup.timer"
cat > "$BACKUP_TIMER_SERVICE" <<'EOF'
[Unit]
Description=HikRAD nightly backup (FR-51.1)
[Service]
Type=oneshot
ExecStart=/usr/local/bin/hikrad backup now
EOF
cat > "$BACKUP_TIMER" <<'EOF'
[Unit]
Description=HikRAD nightly backup timer
[Timer]
OnCalendar=*-*-* 03:00:00
RandomizedDelaySec=600
Persistent=true
[Install]
WantedBy=timers.target
EOF
systemctl daemon-reload
[ "$NO_START" -eq 1 ] || systemctl enable --now hikrad-backup.timer >/dev/null 2>&1 || true

# --- 12. CLI wrapper -----------------------------------------------------------
install -d -m 0755 "$PREFIX/usr/local/bin"
install -m 0755 "$STAGE_CLI" "$PREFIX/usr/local/bin/hikrad" \
    && log "Installed CLI wrapper: $PREFIX/usr/local/bin/hikrad" \
    || warn "could not install $PREFIX/usr/local/bin/hikrad — copy scripts/hikrad there manually."

# --- 13. install.meta (read back by `hikrad` and by this script's own -----
# port-reuse logic above).
cat > "$META_FILE" <<EOF
# Written by install.sh $(date -u +%Y-%m-%dT%H:%M:%SZ) — read by /usr/local/bin/hikrad.
HIKRAD_HTTP_PORT=$HTTP_PORT
HIKRAD_HTTPS_PORT=$HTTPS_PORT
HIKRAD_DELIVERY_MODE=$HIKRAD_DELIVERY_MODE
HIKRAD_ENV_FILE=$ENV_FILE
EOF

# --- 14. Summary --------------------------------------------------------------
PASSPHRASE="$(grep '^HIKRAD_BACKUP_PASSPHRASE=' "$ENV_FILE" | cut -d= -f2-)"
cat <<EOF

================================================================================
 HikRAD install summary — save this somewhere safe, it is not shown again.
================================================================================
 Panel:               https://<this-server>:$HTTPS_PORT/
 Config:               $ETC_DIR
 Data:                 $VAR_DIR
 Backups:              $BACKUP_DIR
 Backup passphrase:    $PASSPHRASE

 This passphrase encrypts every backup archive (FR-51.1). It also lives in
 $ENV_FILE for unattended nightly backups, but a stolen backup file alone
 decrypts nothing without it — write it down or store it in a password
 manager. If BOTH this copy and hikrad.env are lost, existing backups are
 permanently unrecoverable by design (no vendor escrow).

 No admin account exists yet — open the panel now and complete the
 first-run wizard (license, admin account, branding, NAS, profile). Until
 then the panel has no way in, which is deliberate: there is no default
 password to change later.
================================================================================

Manage the stack with: hikrad {status|logs|backup|restore|update|doctor|passwd}
EOF
