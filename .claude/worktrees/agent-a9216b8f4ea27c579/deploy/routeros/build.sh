#!/usr/bin/env bash
# build.sh — builds and saves the ONE RouterOS container image (v4-2,
# FR-120, Decision 58). This is the one legitimate use of the docker CLI
# left anywhere in this repo: it is a BUILD TOOL for producing the single
# OCI image RouterOS's own container feature consumes, not a delivery
# mechanism for the Linux install (which uses no Docker at all — see
# scripts/install.sh, Decision 57). gate item 10's `grep -ri docker` check
# expects this to be the only remaining match under deploy/scripts/.github.
#
# Usage: build.sh <version> <out-tar-path> [amd64|arm64]
#   e.g. build.sh v4.2.0 /tmp/hikrad-routeros-v4.2.0-amd64.tar amd64
#
# The output .tar is a plain `docker save` image tar, imported directly by
# RouterOS's `/container add file=...` — no registry push needed (NFR-7).
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
VERSION="${1:?usage: build.sh <version> <out-tar-path> [amd64|arm64]}"
OUT_TAR="${2:?usage: build.sh <version> <out-tar-path> [amd64|arm64]}"
ARCH="${3:-amd64}"

log() { printf '[routeros-build] %s\n' "$*"; }
die() { printf '[routeros-build] ERROR: %s\n' "$*" >&2; exit 1; }

command -v docker >/dev/null 2>&1 || die "docker is required to build the OCI image (it is only ever used as a BUILD TOOL here, never installed on a customer's Linux server)"

case "$ARCH" in
    amd64) PLATFORM=linux/amd64 ;;
    arm64) PLATFORM=linux/arm64 ;;
    *) die "unsupported arch: $ARCH (amd64 or arm64)" ;;
esac

TAG="hikrad-routeros:$VERSION-$ARCH"
log "Building $TAG ($PLATFORM)…"
docker build --platform "$PLATFORM" \
    -f "$ROOT/deploy/routeros/Dockerfile" \
    -t "$TAG" \
    --build-arg "HIKRAD_VERSION=$VERSION" \
    "$ROOT"

log "Saving $OUT_TAR…"
docker save "$TAG" -o "$OUT_TAR"
log "Done: $OUT_TAR ($(du -h "$OUT_TAR" 2>/dev/null | cut -f1))"
log "Import on the router: /container/add file=$(basename "$OUT_TAR") interface=veth1 root-dir=disk1/hikrad — see deploy/routeros/README.md for the full procedure."
