#!/usr/bin/env bash
# HikRAD — one-command installer.
#
#   Online:   curl -fsSL https://raw.githubusercontent.com/Hikari0Des/hikrad-releases/main/install.sh | sudo bash
#   Offline:  put this file and hikrad-vX.Y.Z.tar in the same folder, then
#             sudo bash install.sh            (or: sudo bash install.sh --bundle /path/hikrad-vX.Y.Z.tar)
#
# Uses a local bundle when one is given or found next to it; otherwise
# downloads the latest signed HikRAD release bundle. Either way it hands off to its own
# installer (scripts/install.sh --bundle, FR-81). This file's canonical copy
# lives here (scripts/quick-install.sh) in the private source repo; the
# release pipeline (.github/workflows/release.yml) publishes a copy of it as
# install.sh at the root of Hikari0Des/hikrad-releases on every tag.
#
# Repo split (PRD Decision 56, amending FR-83.1): Hikari0Des/hikrad-releases
# is a SEPARATE, intentionally public repository that contains NO HikRAD
# source — only this thin wrapper and signed release bundle assets (compiled
# binaries + install scripts + migrations, never .go/.tsx source or an OCI
# image — Docker is cancelled as the delivery mechanism, v4-2 Decision 57).
# The real source tree stays in the private Hikari0Des/HikRAD repo, which
# customers and resellers never get access to (FR-83.1, unchanged). If you
# can read this comment from a git clone of the source repo, that clone
# requires private-repo access — this script itself has none of it.
#
# The actual integrity guarantee here is NOT this script or GitHub's TLS —
# it's the bundle's own Ed25519 signature, checked by scripts/install.sh
# --bundle (scripts/verify-bundle.sh) before a single byte is extracted into
# place. This wrapper is a thin, unauthenticated bootstrap on top of that
# already-existing mechanism, not a new trust boundary.
set -euo pipefail

REPO="Hikari0Des/hikrad-releases"
BLUE=$'\033[1;34m'; RED=$'\033[1;31m'; GREEN=$'\033[1;32m'; RESET=$'\033[0m'
say() { echo "${BLUE}==>${RESET} $*"; }
die() { echo "${RED}error:${RESET} $*" >&2; exit 1; }

[ "$(id -u)" -eq 0 ] || die "run as root — pipe into 'sudo bash':
  curl -fsSL https://raw.githubusercontent.com/${REPO}/main/install.sh | sudo bash"

# --bundle FILE, or a hikrad-v*.tar next to this script / in the current
# directory, means an offline install: no download at all (NFR-7 — a customer
# site may have no internet on the server). Every other argument is passed
# through to scripts/install.sh (--domain, --no-start, …).
local_bundle=""
pass=()
while [ $# -gt 0 ]; do
  case "$1" in
    --bundle) local_bundle="${2:?--bundle needs a path to hikrad-vX.Y.Z.tar}"; shift 2 ;;
    *) pass+=("$1"); shift ;;
  esac
done
if [ -z "$local_bundle" ]; then
  here=""
  [ -n "${BASH_SOURCE[0]:-}" ] && [ -f "${BASH_SOURCE[0]}" ] && here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
  for dir in "$here" "$PWD"; do
    [ -n "$dir" ] || continue
    found="$(ls -1 "$dir"/hikrad-v*.tar 2>/dev/null | sort -V | tail -n1 || true)"
    if [ -n "$found" ]; then local_bundle="$found"; break; fi
  done
fi

command -v tar >/dev/null 2>&1 || die "'tar' is required but not installed."
[ -n "$local_bundle" ] || command -v curl >/dev/null 2>&1 || die "'curl' is required to download a release (or pass --bundle with a local hikrad-vX.Y.Z.tar)."

workdir="$(mktemp -d)"
trap 'rm -rf "$workdir"' EXIT

if [ -n "$local_bundle" ]; then
  [ -f "$local_bundle" ] || die "bundle not found: $local_bundle"
  version="$(basename "$local_bundle" | sed -E 's/^hikrad-(.*)\.tar$/\1/')"
  say "Installing from local bundle $(basename "$local_bundle") (offline — nothing is downloaded)"
  cp "$local_bundle" "$workdir/hikrad.tar"
else
say "Finding the latest HikRAD release…"
api="https://api.github.com/repos/${REPO}/releases/latest"
asset_url="$(curl -fsSL --connect-timeout 10 --max-time 20 "$api" 2>/dev/null \
  | grep -oE '"browser_download_url":[[:space:]]*"[^"]*hikrad-[^"]*\.tar"' \
  | head -n1 | sed -E 's/.*"(https[^"]*)".*/\1/' || true)"
[ -n "$asset_url" ] || die "couldn't find a published release at ${api} — has one been tagged yet?"

version="$(basename "$asset_url" | sed -E 's/^hikrad-(.*)\.tar$/\1/')"
say "Latest release: HikRAD ${version}"
say "Downloading ${asset_url##*/} — a full offline bundle (binaries, frontend build, FreeRADIUS config, migrations; NFR-7, no internet needed after this)…"
curl -fSL --progress-bar --connect-timeout 10 --speed-limit 10240 --speed-time 60 --max-time 3600 \
  "$asset_url" -o "$workdir/hikrad.tar"
fi

say "Extracting the installer…"
# Only scripts/install.sh is pulled out here, just to bootstrap — it does its
# OWN full re-extraction + Ed25519 signature verification of the whole tar
# (scripts/verify-bundle.sh) before anything from it is trusted or used. GNU
# tar packs "-C dir ." entries with a leading "./"; try both forms rather
# than assume one.
tar -xf "$workdir/hikrad.tar" -C "$workdir" ./scripts/install.sh 2>/dev/null \
  || tar -xf "$workdir/hikrad.tar" -C "$workdir" scripts/install.sh 2>/dev/null \
  || die "the downloaded release bundle has no scripts/install.sh — it may be corrupted or incomplete, try again"

say "Running the HikRAD installer (verifies the bundle's signature before touching anything)…"
echo
bash "$workdir/scripts/install.sh" --bundle "$workdir/hikrad.tar" ${pass[@]+"${pass[@]}"}
echo
echo "${GREEN}==>${RESET} HikRAD ${version} installation finished."
