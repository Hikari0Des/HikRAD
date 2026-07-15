#!/usr/bin/env bash
# install.sh — HikRAD production installer (FR-49.1/49.4/49.5; Phase 1 Agent
# A, finalized Phase 5).
#
# Run as root from an unpacked HikRAD release/checkout on Ubuntu 22.04/24.04:
#   sudo ./scripts/install.sh [--no-start] [--domain panel.example.isp]
#
# What it does on a clean server:
#   1. Verifies the OS (Ubuntu 22.04/24.04) and warns (never blocks — small
#      labs/VMs are legitimate) if below the NFR-3 hardware tier.
#   2. Installs Docker Engine + Compose plugin if missing.
#   3. Creates /opt/hikrad/{data,backups,licenses} and generates secrets
#      into /opt/hikrad/.env (HIKRAD_ENV=prod), including a backup
#      passphrase printed ONCE below — write it down (FR-51.1, deliberately
#      unrecoverable if both copies are lost).
#   4. Installs the `hikrad` CLI wrapper to /usr/local/bin and a nightly
#      backup cron entry (FR-51.1).
#   5. With --domain: points Caddy at that hostname for Let's Encrypt;
#      without it, Caddy's default self-signed cert is used (NFR-4, FR-49.5).
#   6. Starts the stack: docker compose up -d --build. The first-run wizard
#      (license, admin, branding, NAS, profile) then runs at the panel URL.
#
# Idempotent: re-running against an existing install NEVER touches data/. It
# offers an update/repair menu instead (interactive) or prints guidance and
# exits 2 (non-interactive / scripted re-run, e.g. CI's install.sh self-test).
#
# Test/override knobs (not for production use):
#   HIKRAD_ROOT           install root (default /opt/hikrad)
#   HIKRAD_SKIP_OS_CHECK  =1 skips the Ubuntu version gate
#   HIKRAD_SKIP_DOCKER    =1 skips Docker presence/install
#   --no-start            do everything except `docker compose up`
#   --domain <fqdn>       configure Caddy for Let's Encrypt on this hostname

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
HIKRAD_ROOT="${HIKRAD_ROOT:-/opt/hikrad}"
NO_START=0
DOMAIN=""

while [ $# -gt 0 ]; do
    case "$1" in
        --no-start) NO_START=1; shift ;;
        --domain) DOMAIN="${2:?--domain requires a hostname}"; shift 2 ;;
        -h|--help) sed -n '2,29p' "$0"; exit 0 ;;
        *) echo "error: unknown argument '$1' (see --help)" >&2; exit 1 ;;
    esac
done

log()  { printf '[hikrad-install] %s\n' "$*"; }
die()  { printf '[hikrad-install] ERROR: %s\n' "$*" >&2; exit 1; }

# --- 0. Existing install? Offer update/repair instead of reinstalling. ------
if [ -e "$HIKRAD_ROOT/.env" ] || [ -e "$HIKRAD_ROOT/install.meta" ]; then
    log "An existing HikRAD installation was found at $HIKRAD_ROOT — data/ is untouched."
    if [ -t 0 ] && [ -t 1 ]; then
        cat <<EOF

What would you like to do?
  1) Update to the latest version (hikrad update — takes a pre-update backup)
  2) Repair: re-run this installer's non-destructive steps (CLI wrapper,
     cron entry) without touching secrets or data
  3) Show status and exit
  4) Exit

EOF
        read -r -p "Choice [1-4]: " choice
        case "$choice" in
            1) exec /usr/local/bin/hikrad update ;;
            2) log "Repairing CLI wrapper + cron entry only…" ;; # falls through to steps 3b/3c below
            3) exec /usr/local/bin/hikrad status ;;
            *) log "Exiting. Nothing changed."; exit 0 ;;
        esac
    else
        log "Non-interactive re-run: not making changes. Use one of:"
        log "    hikrad update             # versioned update, pre-backed-up"
        log "    hikrad status              # show service health"
        exit 2
    fi
fi

REPAIR_ONLY=0
[ "${choice:-}" = "2" ] && REPAIR_ONLY=1

if [ "$REPAIR_ONLY" -eq 0 ]; then
    # --- 1. OS + privilege + hardware checks ---------------------------------
    if [ "${HIKRAD_SKIP_OS_CHECK:-0}" != "1" ]; then
        [ "$(id -u)" -eq 0 ] || die "must run as root (sudo ./scripts/install.sh)"
        [ -r /etc/os-release ] || die "cannot read /etc/os-release — unsupported OS"
        # shellcheck disable=SC1091
        . /etc/os-release
        if [ "${ID:-}" != "ubuntu" ] || { [ "${VERSION_ID:-}" != "22.04" ] && [ "${VERSION_ID:-}" != "24.04" ]; }; then
            die "unsupported OS: ${PRETTY_NAME:-unknown}. HikRAD supports Ubuntu 22.04 / 24.04 LTS."
        fi
        log "OS check passed: $PRETTY_NAME"

        # NFR-3 tier: warn, never block (small labs / VMs are legitimate — this
        # IS the override; there is no separate flag to bypass a check that
        # never blocks in the first place).
        cpus="$(nproc 2>/dev/null || echo 0)"
        mem_kb="$(awk '/MemTotal/ {print $2}' /proc/meminfo 2>/dev/null || echo 0)"
        disk_gb="$(df -BG --output=avail / 2>/dev/null | tail -1 | tr -dc '0-9' || echo 0)"
        [ "$cpus" -ge 4 ]            || log "WARNING: $cpus vCPU found; the supported tier is 4 vCPU (NFR-3)."
        [ "$mem_kb" -ge 7000000 ]    || log "WARNING: <8 GB RAM found; the supported tier is 8 GB (NFR-3)."
        [ "${disk_gb:-0}" -ge 180 ]  || log "WARNING: <200 GB free disk; the supported tier is 200 GB SSD (NFR-3)."
    fi

    # --- 2. Docker Engine + Compose plugin -----------------------------------
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

    command -v gpg >/dev/null 2>&1 || log "WARNING: gpg not found — install gnupg before your first 'hikrad backup now' (backups are passphrase-encrypted, FR-51.1)."

    # --- 3. Layout + secrets --------------------------------------------------
    log "Creating $HIKRAD_ROOT/{data,backups,licenses}…"
    mkdir -p "$HIKRAD_ROOT/data" "$HIKRAD_ROOT/backups" "$HIKRAD_ROOT/licenses"
    chmod 750 "$HIKRAD_ROOT"

    log "Generating secrets into $HIKRAD_ROOT/.env…"
    "$SCRIPT_DIR/gen-env.sh" --env prod "$HIKRAD_ROOT/.env"
    {
        echo "HIKRAD_DATA_DIR=$HIKRAD_ROOT/data"
        echo "HIKRAD_BACKUP_DIR=$HIKRAD_ROOT/backups"
    } >> "$HIKRAD_ROOT/.env"

    # --- 3b. TLS: Let's Encrypt if --domain given, else self-signed default --
    if [ -n "$DOMAIN" ]; then
        log "Configuring Caddy for Let's Encrypt on $DOMAIN…"
        CADDYFILE="$REPO_DIR/deploy/caddy/Caddyfile"
        cp "$CADDYFILE" "$CADDYFILE.orig"
        sed -i \
            -e "s/^localhost:443 {/${DOMAIN}:443 {/" \
            -e '/^\ttls internal$/d' \
            "$CADDYFILE"
        log "Caddyfile updated (original saved as Caddyfile.orig). Point $DOMAIN's DNS at this server before starting, or Let's Encrypt issuance will retry until it can."
    else
        log "No --domain given: using Caddy's self-signed cert (browsers will warn until you trust its local CA, or re-run with --domain later)."
    fi

    # Record where the compose sources live so the `hikrad` wrapper finds them.
    cat > "$HIKRAD_ROOT/install.meta" <<EOF
# Written by install.sh $(date -u +%Y-%m-%dT%H:%M:%SZ) — read by /usr/local/bin/hikrad.
HIKRAD_CHECKOUT=$REPO_DIR
HIKRAD_COMPOSE_FILE=$REPO_DIR/deploy/compose.yml
HIKRAD_ENV_FILE=$HIKRAD_ROOT/.env
EOF
fi

# --- 3d. acct-spill ownership (idempotent; also runs on repair) -------------
# hikrad-acct runs as a fixed non-root uid (10002, deploy/docker/acct.Dockerfile)
# and writes its crash-safe spill WAL into this bind mount. Create + chown it
# ourselves, the same way cmd_tunnel's token file already does for its own
# bind mount — otherwise Docker auto-creates it root-owned on the first
# `compose up` and the non-root container can never write to it (this was a
# known, documented crash-loop bug; see CLAUDE.md's "known deployment bug").
# Runs on repair too, so an install broken by this bug before the fix landed
# self-heals on the next repair without a full reinstall.
mkdir -p "$HIKRAD_ROOT/data/acct-spill"
chown 10002:10002 "$HIKRAD_ROOT/data/acct-spill"

# --- 4. CLI wrapper + nightly backup cron (idempotent; also runs on repair) --
if install -m 0755 "$SCRIPT_DIR/hikrad" /usr/local/bin/hikrad 2>/dev/null; then
    log "Installed CLI wrapper: /usr/local/bin/hikrad"
else
    log "WARNING: could not install /usr/local/bin/hikrad — copy scripts/hikrad there manually."
fi

if command -v crontab >/dev/null 2>&1; then
    CRON_MARK="# hikrad nightly backup (FR-51.1, installed by install.sh)"
    CRON_LINE="0 3 * * * HIKRAD_META=$HIKRAD_ROOT/install.meta /usr/local/bin/hikrad backup now >> $HIKRAD_ROOT/backup.log 2>&1"
    if ! crontab -l 2>/dev/null | grep -qF "$CRON_MARK"; then
        ( crontab -l 2>/dev/null; echo "$CRON_MARK"; echo "$CRON_LINE" ) | crontab -
        log "Installed nightly backup cron entry (03:00, retention from HIKRAD_BACKUP_RETENTION in .env, default 14)."
    fi
else
    log "WARNING: no crontab available — schedule 'hikrad backup now' yourself (e.g. a systemd timer)."
fi

if [ "$REPAIR_ONLY" -eq 1 ]; then
    log "Repair complete. Nothing else was changed."
    exit 0
fi

# --- 5. Bring the stack up ----------------------------------------------------
if [ "$NO_START" -eq 1 ]; then
    log "--no-start given: skipping compose up. Start later with: hikrad up"
else
    log "Starting the stack (first build can take a few minutes)…"
    docker compose --env-file "$HIKRAD_ROOT/.env" -f "$REPO_DIR/deploy/compose.yml" up -d --build
    log "Stack started. Panel: https://<this-server>/  (first-run wizard: license, admin, branding, NAS, profile)"
fi

# --- 6. Install summary: the backup passphrase, shown exactly once. ---------
PASSPHRASE="$(grep '^HIKRAD_BACKUP_PASSPHRASE=' "$HIKRAD_ROOT/.env" | cut -d= -f2-)"
cat <<EOF

================================================================================
 HikRAD install summary — save this somewhere safe, it is not shown again.
================================================================================
 Install root:        $HIKRAD_ROOT
 Backup passphrase:   $PASSPHRASE

 This passphrase encrypts every backup archive (FR-51.1). It also lives in
 $HIKRAD_ROOT/.env for unattended nightly backups, but a stolen backup file
 alone decrypts nothing without it — write it down or store it in a password
 manager. If BOTH this copy and .env are lost, existing backups are
 permanently unrecoverable by design (no vendor escrow).
================================================================================

Next: open the panel and complete the first-run wizard.
Manage the stack with: hikrad {up|down|status|logs|backup|restore|update|tunnel}
EOF
