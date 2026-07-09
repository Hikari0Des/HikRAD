#!/usr/bin/env bash
# install.sh v0 — HikRAD production installer (FR-49.1, FR-49.4; Phase 1, Agent A).
#
# Run as root from an unpacked HikRAD release/checkout on Ubuntu 22.04/24.04:
#   sudo ./scripts/install.sh [--no-start]
#
# What it does:
#   1. Verifies the OS (Ubuntu 22.04/24.04) and warns if below the NFR-3
#      hardware tier (4 vCPU / 8 GB RAM / 200 GB disk).
#   2. Installs Docker Engine + Compose plugin if missing.
#   3. Creates /opt/hikrad/{data,backups,licenses} and generates secrets
#      into /opt/hikrad/.env (HIKRAD_ENV=prod).
#   4. Installs the `hikrad` CLI wrapper to /usr/local/bin.
#   5. Starts the stack: docker compose up -d --build.
#
# Idempotent: re-running against an existing install ABORTS with a message
# and never touches data/ (the guided update flow ships in Phase 5).
#
# Test/override knobs (not for production use):
#   HIKRAD_ROOT           install root (default /opt/hikrad)
#   HIKRAD_SKIP_OS_CHECK  =1 skips the Ubuntu version gate
#   HIKRAD_SKIP_DOCKER    =1 skips Docker presence/install
#   --no-start            do everything except `docker compose up`

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
HIKRAD_ROOT="${HIKRAD_ROOT:-/opt/hikrad}"
NO_START=0

for arg in "$@"; do
    case "$arg" in
        --no-start) NO_START=1 ;;
        -h|--help) sed -n '2,24p' "$0"; exit 0 ;;
        *) echo "error: unknown argument '$arg' (see --help)" >&2; exit 1 ;;
    esac
done

log()  { printf '[hikrad-install] %s\n' "$*"; }
die()  { printf '[hikrad-install] ERROR: %s\n' "$*" >&2; exit 1; }

# --- 0. Existing install? Abort before touching anything (FR-49.4). ---------
if [ -e "$HIKRAD_ROOT/.env" ] || [ -e "$HIKRAD_ROOT/install.meta" ]; then
    log "An existing HikRAD installation was found at $HIKRAD_ROOT."
    log "This installer will not modify it — your data/ directory is untouched."
    log "The guided update flow ships in a later release; for now use:"
    log "    hikrad down && git -C <checkout> pull && hikrad up"
    exit 2
fi

# --- 1. OS + privilege + hardware checks -------------------------------------
if [ "${HIKRAD_SKIP_OS_CHECK:-0}" != "1" ]; then
    [ "$(id -u)" -eq 0 ] || die "must run as root (sudo ./scripts/install.sh)"
    [ -r /etc/os-release ] || die "cannot read /etc/os-release — unsupported OS"
    # shellcheck disable=SC1091
    . /etc/os-release
    if [ "${ID:-}" != "ubuntu" ] || { [ "${VERSION_ID:-}" != "22.04" ] && [ "${VERSION_ID:-}" != "24.04" ]; }; then
        die "unsupported OS: ${PRETTY_NAME:-unknown}. HikRAD supports Ubuntu 22.04 / 24.04 LTS."
    fi
    log "OS check passed: $PRETTY_NAME"

    # NFR-3 tier: warn, don't block (small labs / VMs are legitimate).
    cpus="$(nproc 2>/dev/null || echo 0)"
    mem_kb="$(awk '/MemTotal/ {print $2}' /proc/meminfo 2>/dev/null || echo 0)"
    disk_gb="$(df -BG --output=avail / 2>/dev/null | tail -1 | tr -dc '0-9' || echo 0)"
    [ "$cpus" -ge 4 ]            || log "WARNING: $cpus vCPU found; the supported tier is 4 vCPU (NFR-3)."
    [ "$mem_kb" -ge 7000000 ]    || log "WARNING: <8 GB RAM found; the supported tier is 8 GB (NFR-3)."
    [ "${disk_gb:-0}" -ge 180 ]  || log "WARNING: <200 GB free disk; the supported tier is 200 GB SSD (NFR-3)."
fi

# --- 2. Docker Engine + Compose plugin ---------------------------------------
if [ "${HIKRAD_SKIP_DOCKER:-0}" != "1" ]; then
    if ! command -v docker >/dev/null 2>&1; then
        log "Docker not found — installing Docker Engine (get.docker.com)…"
        log "(Offline servers: install Docker from your OS media first, then re-run.)"
        curl -fsSL https://get.docker.com | sh
    fi
    docker compose version >/dev/null 2>&1 \
        || die "Docker Compose plugin missing. Install docker-compose-plugin and re-run."
    log "Docker OK: $(docker --version)"
fi

# --- 3. Layout + secrets ------------------------------------------------------
log "Creating $HIKRAD_ROOT/{data,backups,licenses}…"
mkdir -p "$HIKRAD_ROOT/data" "$HIKRAD_ROOT/backups" "$HIKRAD_ROOT/licenses"
chmod 750 "$HIKRAD_ROOT"

log "Generating secrets into $HIKRAD_ROOT/.env…"
"$SCRIPT_DIR/gen-env.sh" --env prod "$HIKRAD_ROOT/.env"
echo "HIKRAD_DATA_DIR=$HIKRAD_ROOT/data" >> "$HIKRAD_ROOT/.env"

# Record where the compose sources live so the `hikrad` wrapper finds them.
cat > "$HIKRAD_ROOT/install.meta" <<EOF
# Written by install.sh $(date -u +%Y-%m-%dT%H:%M:%SZ) — read by /usr/local/bin/hikrad.
HIKRAD_CHECKOUT=$REPO_DIR
HIKRAD_COMPOSE_FILE=$REPO_DIR/deploy/compose.yml
HIKRAD_ENV_FILE=$HIKRAD_ROOT/.env
EOF

if install -m 0755 "$SCRIPT_DIR/hikrad" /usr/local/bin/hikrad 2>/dev/null; then
    log "Installed CLI wrapper: /usr/local/bin/hikrad"
else
    log "WARNING: could not install /usr/local/bin/hikrad — copy scripts/hikrad there manually."
fi

# --- 4. Bring the stack up ----------------------------------------------------
if [ "$NO_START" -eq 1 ]; then
    log "--no-start given: skipping compose up. Start later with: hikrad up"
else
    log "Starting the stack (first build can take a few minutes)…"
    docker compose --env-file "$HIKRAD_ROOT/.env" -f "$REPO_DIR/deploy/compose.yml" up -d --build
    log "Stack started. Panel: https://<this-server>/  (self-signed cert by default)"
fi

log "Install complete. State: $HIKRAD_ROOT  |  Manage with: hikrad {up|down|status|logs}"
