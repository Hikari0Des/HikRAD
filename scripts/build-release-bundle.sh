#!/usr/bin/env bash
# build-release-bundle.sh — assembles and signs a hikrad-vX.Y.Z.tar release
# bundle (v4-2, FR-119: reworked from Docker images to native binaries — the
# release bundle carries binaries and assets, not images, per Decision 57).
# Called by the CI release job; also runnable locally for a rehearsal build.
#
# Usage: build-release-bundle.sh <version> <out-tar-path>
#   e.g. build-release-bundle.sh v4.2.0 /tmp/hikrad-v4.2.0.tar
#
# Env:
#   RELEASE_SIGNING_KEY_FILE default: scripts/release-signing/dev-release-key.pem
#                            (use the real offline key for an actual shipment —
#                            see scripts/release-signing/README.md)
#   HIKRAD_SKIP_BUILD        =1 skips `go build`/`npm run build` entirely
#                            (bin/ and www/ ship empty) — for fast local
#                            rehearsal of the assembly+signing steps and for
#                            the gate script, mirrors the old
#                            HIKRAD_SKIP_IMAGE_BUILD knob.
#   HIKRAD_GOOS/HIKRAD_GOARCH  cross-compile target for the four Go binaries
#                            (default: linux/amd64 — the only server target;
#                            the RouterOS image is a SEPARATE build,
#                            deploy/routeros/build.sh, never this script).
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
VERSION="${1:?usage: build-release-bundle.sh <version> <out-tar-path>}"
OUT_TAR="${2:?usage: build-release-bundle.sh <version> <out-tar-path>}"
RELEASE_SIGNING_KEY_FILE="${RELEASE_SIGNING_KEY_FILE:-$ROOT/scripts/release-signing/dev-release-key.pem}"
GOOS="${HIKRAD_GOOS:-linux}"
GOARCH="${HIKRAD_GOARCH:-amd64}"

log() { printf '[build-release-bundle] %s\n' "$*"; }
die() { printf '[build-release-bundle] ERROR: %s\n' "$*" >&2; exit 1; }

[ -f "$RELEASE_SIGNING_KEY_FILE" ] || die "signing key not found: $RELEASE_SIGNING_KEY_FILE"
command -v openssl >/dev/null 2>&1 || die "openssl is required"
command -v sha256sum >/dev/null 2>&1 || die "sha256sum is required"

STAGE="$(mktemp -d)"
trap 'rm -rf "$STAGE"' EXIT
mkdir -p "$STAGE/bin" "$STAGE/www"

# --- binaries ------------------------------------------------------------------
if [ "${HIKRAD_SKIP_BUILD:-0}" = "1" ]; then
  log "HIKRAD_SKIP_BUILD=1: skipping go build/npm build (bin/ and www/ ship empty — rehearsal/gate mode only)"
else
  command -v go >/dev/null 2>&1 || die "go is required (or set HIKRAD_SKIP_BUILD=1)"
  ( cd "$ROOT/backend" && for svc in hikrad-api hikrad-acct hikrad-monitor hikrad-updaterd; do
      log "Building $svc ($GOOS/$GOARCH)…"
      ldflags="-s -w"
      [ "$svc" = "hikrad-api" ] && ldflags="-s -w -X github.com/hikrad/hikrad/internal/platform/setupapi.appVersion=$VERSION"
      GOOS="$GOOS" GOARCH="$GOARCH" CGO_ENABLED=0 go build -trimpath -ldflags="$ldflags" -o "$STAGE/bin/$svc" "./cmd/$svc"
    done )

  if command -v npm >/dev/null 2>&1; then
    log "Building frontend (panel + portal)…"
    ( cd "$ROOT/frontend" && npm ci && npm run build --workspaces --if-present )
    cp -r "$ROOT/frontend/panel/dist" "$STAGE/www/panel"
    cp -r "$ROOT/frontend/portal/dist" "$STAGE/www/portal"
  else
    die "npm is required to build the frontend (or set HIKRAD_SKIP_BUILD=1)"
  fi
fi

# --- runtime config + installer scripts ---------------------------------------
log "Copying FreeRADIUS config, nginx template, systemd units…"
cp -r "$ROOT/deploy/freeradius" "$STAGE/freeradius"

mkdir -p "$STAGE/nginx"
cp "$ROOT/deploy/nginx/hikrad.conf.template" "$STAGE/nginx/"

mkdir -p "$STAGE/systemd"
cp "$ROOT"/deploy/systemd/*.service "$STAGE/systemd/"

log "Copying installer scripts (install.sh, hikrad, gen-env.sh, verify-bundle.sh, migrate-to-native.sh)…"
mkdir -p "$STAGE/scripts"
cp "$ROOT/scripts/install.sh" "$ROOT/scripts/hikrad" "$ROOT/scripts/gen-env.sh" \
   "$ROOT/scripts/verify-bundle.sh" "$ROOT/scripts/migrate-to-native.sh" "$STAGE/scripts/"

log "Copying migrations…"
mkdir -p "$STAGE/migrations"
cp "$ROOT"/backend/migrations/*.up.sql "$STAGE/migrations/"

echo "$VERSION" > "$STAGE/VERSION"

# --- manifest ------------------------------------------------------------------
schema_version="$(ls "$ROOT"/backend/migrations/*.up.sql | sed -E 's#.*/([0-9]+)_.*#\1#' | sort -n | tail -1)"
git_commit="$(git -C "$ROOT" rev-parse HEAD 2>/dev/null || echo unknown)"
built_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
{
  printf '{\n'
  printf '  "version": "%s",\n' "$VERSION"
  printf '  "git_commit": "%s",\n' "$git_commit"
  printf '  "built_at": "%s",\n' "$built_at"
  printf '  "schema_version": "%s",\n' "$schema_version"
  printf '  "target": "%s/%s",\n' "$GOOS" "$GOARCH"
  printf '  "binaries": ["hikrad-api", "hikrad-acct", "hikrad-monitor", "hikrad-updaterd"]\n'
  printf '}\n'
} > "$STAGE/manifest.json"

# --- checksums + signature ----------------------------------------------------
log "Computing SHA256SUMS…"
( cd "$STAGE" && find . -type f ! -name SHA256SUMS ! -name SHA256SUMS.sig -exec sha256sum {} + ) > "$STAGE/SHA256SUMS"

log "Signing SHA256SUMS…"
openssl pkeyutl -sign -inkey "$RELEASE_SIGNING_KEY_FILE" -rawin \
  -in "$STAGE/SHA256SUMS" -out "$STAGE/SHA256SUMS.sig"

# --- verify our own output before shipping it ---------------------------------
bash "$ROOT/scripts/verify-bundle.sh" "$STAGE"

# --- pack ----------------------------------------------------------------------
log "Packing $OUT_TAR…"
tar -cf "$OUT_TAR" -C "$STAGE" .
log "Done: $OUT_TAR ($(du -h "$OUT_TAR" 2>/dev/null | cut -f1))"
