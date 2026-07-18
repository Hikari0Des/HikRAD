#!/usr/bin/env bash
# build-release-bundle.sh — assembles and signs a hikrad-vX.Y.Z.tar release
# bundle (FR-81.2, phase v2-5 C2). Called by the CI release job (C3); also
# runnable locally for a rehearsal build.
#
# Usage: build-release-bundle.sh <version> <out-tar-path>
#   e.g. build-release-bundle.sh v1.2.0 /tmp/hikrad-v1.2.0.tar
#
# Env:
#   HIKRAD_REGISTRY          default: ghcr.io/hikrad
#   RELEASE_SIGNING_KEY_FILE default: scripts/release-signing/dev-release-key.pem
#                            (use the real offline key for an actual shipment —
#                            see scripts/release-signing/README.md)
#   HIKRAD_SKIP_IMAGE_BUILD  =1 skips `docker build`/`docker pull`/`docker save`
#                            entirely (images/ ships empty) — for fast local
#                            rehearsal of the non-image assembly+signing steps
#                            and for the gate script, which does not need a
#                            real multi-GB image set to prove the mechanism.
#   HIKRAD_PUSH_IMAGES       =1 also `docker push`es each of the 4 HikRAD
#                            images to HIKRAD_REGISTRY after building (the CI
#                            release job sets this; a local rehearsal build
#                            normally does not — requires `docker login`
#                            against the registry beforehand, this script
#                            never logs in itself).
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
VERSION="${1:?usage: build-release-bundle.sh <version> <out-tar-path>}"
OUT_TAR="${2:?usage: build-release-bundle.sh <version> <out-tar-path>}"
HIKRAD_REGISTRY="${HIKRAD_REGISTRY:-ghcr.io/hikrad}"
RELEASE_SIGNING_KEY_FILE="${RELEASE_SIGNING_KEY_FILE:-$ROOT/scripts/release-signing/dev-release-key.pem}"

log() { printf '[build-release-bundle] %s\n' "$*"; }
die() { printf '[build-release-bundle] ERROR: %s\n' "$*" >&2; exit 1; }

[ -f "$RELEASE_SIGNING_KEY_FILE" ] || die "signing key not found: $RELEASE_SIGNING_KEY_FILE"
command -v openssl >/dev/null 2>&1 || die "openssl is required"
command -v sha256sum >/dev/null 2>&1 || die "sha256sum is required"

STAGE="$(mktemp -d)"
trap 'rm -rf "$STAGE"' EXIT
mkdir -p "$STAGE/images"

# --- images ------------------------------------------------------------------
# HikRAD's own 4 images + the pinned third-party images the stack needs, so
# the bundle is fully offline-capable (NFR-7) — not just HikRAD's own code.
THIRD_PARTY_IMAGES="timescale/timescaledb:latest-pg16 redis:7-alpine freeradius/freeradius-server:3.2.3"
declare -A HIKRAD_IMAGES=(
  [hikrad-api]=deploy/docker/api.Dockerfile
  [hikrad-acct]=deploy/docker/acct.Dockerfile
  [hikrad-monitor]=deploy/docker/monitor.Dockerfile
  [hikrad-caddy]=deploy/docker/caddy.Dockerfile
)

if [ "${HIKRAD_SKIP_IMAGE_BUILD:-0}" = "1" ]; then
  log "HIKRAD_SKIP_IMAGE_BUILD=1: skipping docker build/pull/save (images/ ships empty — rehearsal/gate mode only)"
else
  command -v docker >/dev/null 2>&1 || die "docker is required (or set HIKRAD_SKIP_IMAGE_BUILD=1)"
  for name in "${!HIKRAD_IMAGES[@]}"; do
    dockerfile="${HIKRAD_IMAGES[$name]}"
    tag="$HIKRAD_REGISTRY/$name:$VERSION"
    log "Building $tag ($dockerfile)…"
    docker build -f "$ROOT/$dockerfile" -t "$tag" \
      --build-arg "HIKRAD_VERSION=$VERSION" "$ROOT"
    if [ "${HIKRAD_PUSH_IMAGES:-0}" = "1" ]; then
      log "Pushing $tag…"
      docker push "$tag"
    fi
    log "Saving $name.tar…"
    docker save "$tag" -o "$STAGE/images/$name.tar"
  done
  for image in $THIRD_PARTY_IMAGES; do
    log "Pulling $image…"
    docker pull "$image"
    fname="$(echo "$image" | tr '/:' '__')"
    log "Saving ${fname}.tar…"
    docker save "$image" -o "$STAGE/images/${fname}.tar"
  done
fi

# --- rendered compose + runtime config ---------------------------------------
log "Rendering compose.yml (image: tags, no build:)…"
HIKRAD_VERSION="$VERSION" HIKRAD_REGISTRY="$HIKRAD_REGISTRY" \
  bash "$ROOT/scripts/render-release-compose.sh" "$ROOT/deploy/compose.yml" "$STAGE/compose.yml"

log "Copying runtime config (freeradius/, caddy/)…"
cp -r "$ROOT/deploy/freeradius" "$STAGE/freeradius"
cp -r "$ROOT/deploy/caddy" "$STAGE/caddy"
# .orig files are install.sh's own --domain backup artifact, never product
# content — must not exist in a fresh checkout, but strip defensively so a
# dev's locally-mutated tree can't leak into a bundle.
find "$STAGE/caddy" -name '*.orig' -delete

log "Copying installer scripts (install.sh, hikrad, gen-env.sh, verify-bundle.sh)…"
mkdir -p "$STAGE/scripts"
cp "$ROOT/scripts/install.sh" "$ROOT/scripts/hikrad" "$ROOT/scripts/gen-env.sh" "$ROOT/scripts/verify-bundle.sh" "$STAGE/scripts/"

# hikrad-updaterd (v2 phase 7, FR-86, C1): a static binary, not a shell
# script, shipped in the same scripts/ directory as the CLI wrapper —
# install.sh looks for it at $SCRIPTS_SRC_DIR/hikrad-updaterd and installs
# it verbatim in bundle mode (never builds from source there, matching the
# rest of this delivery model). Cross-compiled for the target Ubuntu
# server's architecture regardless of what OS/arch builds the bundle
# itself — CGO_ENABLED=0 keeps it a single static binary with no libc
# dependency on the target, same posture every other HikRAD binary already
# has inside its container image.
if [ "${HIKRAD_SKIP_IMAGE_BUILD:-0}" = "1" ]; then
  log "HIKRAD_SKIP_IMAGE_BUILD=1: skipping hikrad-updaterd build too (rehearsal/gate mode only — install.sh degrades gracefully with no binary present)"
else
  command -v go >/dev/null 2>&1 || die "go is required to build hikrad-updaterd (or set HIKRAD_SKIP_IMAGE_BUILD=1 for a rehearsal build without it)"
  log "Building hikrad-updaterd (linux/amd64)…"
  ( cd "$ROOT/backend" && GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o "$STAGE/scripts/hikrad-updaterd" ./cmd/hikrad-updaterd )
  chmod 0755 "$STAGE/scripts/hikrad-updaterd"
fi

log "Copying migrations…"
mkdir -p "$STAGE/migrations"
cp "$ROOT"/backend/migrations/*.up.sql "$STAGE/migrations/"

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
  printf '  "registry": "%s",\n' "$HIKRAD_REGISTRY"
  printf '  "images": [%s]\n' "$(printf '"%s", ' "${!HIKRAD_IMAGES[@]}" | sed 's/, $//')"
  printf '}\n'
} > "$STAGE/manifest.json"

# --- checksums + signature ----------------------------------------------------
log "Computing SHA256SUMS…"
( cd "$STAGE" && find . -type f ! -name SHA256SUMS ! -name SHA256SUMS.sig -exec sha256sum {} + ) > "$STAGE/SHA256SUMS"

log "Signing SHA256SUMS…"
openssl pkeyutl -sign -inkey "$RELEASE_SIGNING_KEY_FILE" -rawin \
  -in "$STAGE/SHA256SUMS" -out "$STAGE/SHA256SUMS.sig"

# --- verify our own output before shipping it ---------------------------------
# A bundle-builder that produces a bundle it wouldn't itself accept is worse
# than no signing at all — prove it round-trips before anyone downloads it.
bash "$ROOT/scripts/verify-bundle.sh" "$STAGE"

# --- pack ----------------------------------------------------------------------
log "Packing $OUT_TAR…"
tar -cf "$OUT_TAR" -C "$STAGE" .
log "Done: $OUT_TAR ($(du -h "$OUT_TAR" 2>/dev/null | cut -f1))"
