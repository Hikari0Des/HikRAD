#!/usr/bin/env bash
# install.sh — HikRAD production installer (FR-49.1/49.4/49.5; Phase 1 Agent
# A, finalized Phase 5; bundle mode added v2 phase 5, FR-81.4).
#
# Two modes:
#   Source (dev-only, unchanged):
#     sudo ./scripts/install.sh [--no-start] [--domain panel.example.isp]
#     Run from an unpacked HikRAD checkout — docker compose builds images
#     from backend/deploy/frontend. This is what `make up` uses.
#   Bundle (production — no source tree, no Go/Node toolchain required):
#     sudo ./scripts/install.sh --bundle hikrad-vX.Y.Z.tar [--no-start] [--domain ...]
#     Verifies the bundle's signature (scripts/verify-bundle.sh) BEFORE
#     touching anything, then uses its compose.yml (image: tags, no build:)
#     and runtime config — no image is ever built on the customer's server.
#
# What it does on a clean server:
#   1. Verifies the OS (Ubuntu 22.04/24.04) and warns (never blocks — small
#      labs/VMs are legitimate) if below the NFR-3 hardware tier.
#   2. Installs Docker Engine + Compose plugin if missing.
#   3. Creates /opt/hikrad/{data,backups,licenses} and generates secrets
#      into /opt/hikrad/.env (HIKRAD_ENV=prod), including a backup
#      passphrase printed ONCE below — write it down (FR-51.1, deliberately
#      unrecoverable if both copies are lost). With --bundle: also verifies
#      and stages the release into /opt/hikrad/release/ (FR-81.3/81.4).
#   4. Installs the `hikrad` CLI wrapper to /usr/local/bin and a nightly
#      backup cron entry (FR-51.1).
#   5. With --domain: points Caddy at that hostname for Let's Encrypt;
#      without it, Caddy's default self-signed cert is used (NFR-4, FR-49.5).
#   6. Starts the stack: `docker compose up -d --build` in source mode, or
#      `docker compose up -d` (images already loaded from the bundle, no
#      build) in bundle mode. The first-run wizard (license, admin, branding,
#      NAS, profile) then runs at the panel URL.
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
#   --bundle <path>       install from a signed offline bundle (FR-81) instead
#                         of building images from source — see above

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
HIKRAD_ROOT="${HIKRAD_ROOT:-/opt/hikrad}"
NO_START=0
DOMAIN=""
BUNDLE=""

while [ $# -gt 0 ]; do
    case "$1" in
        --no-start) NO_START=1; shift ;;
        --domain) DOMAIN="${2:?--domain requires a hostname}"; shift 2 ;;
        --bundle) BUNDLE="${2:?--bundle requires a path to a hikrad-vX.Y.Z.tar file}"; shift 2 ;;
        -h|--help) sed -n '2,36p' "$0"; exit 0 ;;
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

# RELEASE_DIR / CHECKOUT_DIR / SCRIPTS_SRC_DIR (v2 phase 5, FR-81.4): where
# compose.yml + runtime config, the "checkout-equivalent" root (VERSION,
# migrations), and the scripts/ to install the CLI wrapper from actually
# live — source mode vs. bundle mode. Resolved once, used by every step
# below (both the REPAIR_ONLY branch, whose idempotent chown/cert fixups run
# unconditionally, and the fresh-install branch). On a repair of an existing
# install, these are read back out of install.meta rather than re-derived —
# a repair run does not necessarily pass --bundle again, and the original
# install's own compose file is the only source of truth for where its
# runtime config actually lives.
if [ -f "$HIKRAD_ROOT/install.meta" ]; then
    # shellcheck disable=SC1091
    . "$HIKRAD_ROOT/install.meta"
    RELEASE_DIR="$(dirname "$HIKRAD_COMPOSE_FILE")"
    CHECKOUT_DIR="$HIKRAD_CHECKOUT"
else
    RELEASE_DIR="$REPO_DIR/deploy"
    CHECKOUT_DIR="$REPO_DIR"
fi
SCRIPTS_SRC_DIR="$SCRIPT_DIR"

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
    # Invoked via bash explicitly, not relying on gen-env.sh's own executable
    # bit: a checkout obtained via GitHub's "Download ZIP" (vs. `git clone`,
    # which preserves git's tracked file mode) strips execute bits from every
    # file, and gen-env.sh silently failing with "Permission denied" was found
    # live in the Phase-5 M4 gate rehearsal on a genuinely fresh Ubuntu box.
    bash "$SCRIPT_DIR/gen-env.sh" --env prod "$HIKRAD_ROOT/.env"
    {
        echo "HIKRAD_DATA_DIR=$HIKRAD_ROOT/data"
        echo "HIKRAD_BACKUP_DIR=$HIKRAD_ROOT/backups"
    } >> "$HIKRAD_ROOT/.env"

    # --- 3a. Bundle staging (FR-81.3/81.4, v2 phase 5): verify, THEN place --
    # A bundle is extracted to its own scratch dir and verified there before
    # anything is copied into $HIKRAD_ROOT — a failed verification leaves the
    # live install (if any) completely untouched (FR-81.3's "no partial
    # effect"). Only on success do RELEASE_DIR/CHECKOUT_DIR/SCRIPTS_SRC_DIR
    # get pointed at the now-staged, verified copy for the rest of this run.
    HIKRAD_DELIVERY_MODE=source
    if [ -n "$BUNDLE" ]; then
        [ -f "$BUNDLE" ] || die "--bundle file not found: $BUNDLE"
        log "Verifying release bundle $BUNDLE before using anything from it…"
        BUNDLE_SCRATCH="$(mktemp -d)"
        trap 'rm -rf "$BUNDLE_SCRATCH"' EXIT
        tar -xf "$BUNDLE" -C "$BUNDLE_SCRATCH"
        bash "$BUNDLE_SCRATCH/scripts/verify-bundle.sh" "$BUNDLE_SCRATCH" \
            || die "bundle verification failed — refusing to install from $BUNDLE (nothing was changed)"

        log "Bundle verified. Loading images into Docker…"
        for image_tar in "$BUNDLE_SCRATCH"/images/*.tar; do
            [ -e "$image_tar" ] || continue   # empty images/ (HIKRAD_SKIP_IMAGE_BUILD rehearsal bundles)
            log "  docker load -i $(basename "$image_tar")"
            docker load -i "$image_tar"
        done

        log "Staging release into $HIKRAD_ROOT/release…"
        mkdir -p "$HIKRAD_ROOT/release"
        cp -r "$BUNDLE_SCRATCH/compose.yml" "$BUNDLE_SCRATCH/freeradius" "$BUNDLE_SCRATCH/caddy" \
              "$BUNDLE_SCRATCH/scripts" "$BUNDLE_SCRATCH/migrations" "$BUNDLE_SCRATCH/manifest.json" \
              "$HIKRAD_ROOT/release/"
        [ -f "$BUNDLE_SCRATCH/VERSION" ] && cp "$BUNDLE_SCRATCH/VERSION" "$HIKRAD_ROOT/release/"
        rm -rf "$BUNDLE_SCRATCH"
        trap - EXIT

        RELEASE_DIR="$HIKRAD_ROOT/release"
        CHECKOUT_DIR="$HIKRAD_ROOT/release"
        SCRIPTS_SRC_DIR="$HIKRAD_ROOT/release/scripts"
        HIKRAD_DELIVERY_MODE=bundle
        log "Release staged at $RELEASE_DIR (delivery mode: bundle — no source tree, no build on this server)."
    fi

    # --- 3b. TLS: Let's Encrypt if --domain given, else self-signed default --
    if [ -n "$DOMAIN" ]; then
        log "Configuring Caddy for Let's Encrypt on $DOMAIN…"
        CADDYFILE="$RELEASE_DIR/caddy/Caddyfile"
        cp "$CADDYFILE" "$CADDYFILE.orig"
        sed -i \
            -e "s/^:443 {/${DOMAIN}:443 {/" \
            -e '/^\ttls \/etc\/caddy\/selfsigned\//d' \
            "$CADDYFILE"
        log "Caddyfile updated (original saved as Caddyfile.orig). Point $DOMAIN's DNS at this server before starting, or Let's Encrypt issuance will retry until it can."
    else
        log "No --domain given: using a self-signed cert (browsers will warn until you trust it, or re-run with --domain later)."
    fi

    # Record where the release sources live so the `hikrad` wrapper finds
    # them, and how this install obtained its images (HIKRAD_DELIVERY_MODE:
    # bundle|source — hikrad update reads this to decide its own default mode).
    cat > "$HIKRAD_ROOT/install.meta" <<EOF
# Written by install.sh $(date -u +%Y-%m-%dT%H:%M:%SZ) — read by /usr/local/bin/hikrad.
HIKRAD_CHECKOUT=$CHECKOUT_DIR
HIKRAD_COMPOSE_FILE=$RELEASE_DIR/compose.yml
HIKRAD_ENV_FILE=$HIKRAD_ROOT/.env
HIKRAD_DELIVERY_MODE=$HIKRAD_DELIVERY_MODE
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

# --- 3d1. clients-generated.conf ownership (idempotent; also runs on repair) -
# hikrad-api runs as a fixed non-root uid (10001, deploy/docker/api.Dockerfile)
# and regenerates this file on every NAS create/update/delete (FR-13.2). A
# plain checkout (git clone as root, or any other method) leaves it
# root-owned 644 — hikrad-api's own uid can't write that either, so NAS
# secrets never actually reach FreeRADIUS's transport layer (found live in
# the Phase-5 M4 gate rehearsal against a real MikroTik: silent timeout,
# nothing in FreeRADIUS's clients-generated.conf despite the NAS existing in
# HikRAD). Same fix shape as acct-spill just below.
chown 10001:10001 "$RELEASE_DIR/freeradius/clients-generated.conf"

# --- 3d2. FreeRADIUS control-socket dir (idempotent; also runs on repair) ---
# Shared between hikrad-api and freeradius (FR-13.2/AC-13a) so hikrad-api can
# trigger a config reload after regenerating the per-NAS client list. Neither
# container's runtime uid is ours to assume (third-party freeradius image;
# hikrad-api's own uid varies by build) — 0777 on this one ephemeral
# IPC-socket directory (no secrets live here) is the pragmatic choice, same
# reasoning as the acct-spill fix just above.
mkdir -p "$HIKRAD_ROOT/data/radius-control"
chmod 777 "$HIKRAD_ROOT/data/radius-control"

# --- 3e. Self-signed TLS cert (idempotent; also runs on repair) -------------
# Covers this server's actual detected IP address(es) plus localhost, not
# just "localhost" — a browser reaching the panel via the server's real LAN
# IP sends no SNI at all (RFC 6066 only applies SNI to hostnames), so a cert
# scoped to a single hostname can never be selected for that connection and
# the handshake fails outright (see deploy/caddy/Caddyfile's own comment;
# found live in the Phase-5 M4 gate rehearsal: curl https://localhost worked,
# a browser hitting the LAN IP got ERR_SSL_PROTOCOL_ERROR). Unused when
# --domain is given (Let's Encrypt takes over) but harmless to have on disk
# either way. Runs on repair too, so an install broken by this bug before the
# fix landed self-heals without a full reinstall — to pick up a changed IP
# later, delete data/caddy-selfsigned/ and re-run.
CERT_DIR="$HIKRAD_ROOT/data/caddy-selfsigned"
if [ ! -f "$CERT_DIR/cert.pem" ]; then
    log "Generating a self-signed TLS certificate covering this server's detected address(es)…"
    mkdir -p "$CERT_DIR"
    SAN="DNS:localhost,IP:127.0.0.1"
    for ip in $(hostname -I 2>/dev/null); do
        SAN="$SAN,IP:$ip"
    done
    openssl req -x509 -nodes -newkey rsa:2048 -days 3650 \
        -keyout "$CERT_DIR/key.pem" -out "$CERT_DIR/cert.pem" \
        -subj "/CN=hikrad" -addext "subjectAltName=$SAN" >/dev/null 2>&1
    chmod 644 "$CERT_DIR/cert.pem"
    chmod 600 "$CERT_DIR/key.pem"
    log "Self-signed cert covers: $SAN"
fi

# --- 4. CLI wrapper + nightly backup cron (idempotent; also runs on repair) --
# SCRIPTS_SRC_DIR is the verified bundle's scripts/ on a fresh --bundle
# install (never the customer's own unverified bootstrap copy of this file —
# see the 3a staging block above), or $SCRIPT_DIR unchanged in every other
# case (source install, or any repair, matching pre-v2-phase-5 behavior).
if install -m 0755 "$SCRIPTS_SRC_DIR/hikrad" /usr/local/bin/hikrad 2>/dev/null; then
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
    if [ "$HIKRAD_DELIVERY_MODE" = "bundle" ]; then
        log "Starting the stack (images already loaded from the bundle — no build)…"
        docker compose --env-file "$HIKRAD_ROOT/.env" -f "$RELEASE_DIR/compose.yml" up -d
    else
        log "Starting the stack (first build can take a few minutes)…"
        docker compose --env-file "$HIKRAD_ROOT/.env" -f "$RELEASE_DIR/compose.yml" up -d --build
    fi
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
