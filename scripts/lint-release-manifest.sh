#!/usr/bin/env bash
# lint-release-manifest.sh — FR-126 gate (v4-3). Inspects an ASSEMBLED
# release bundle (an extracted hikrad-vX.Y.Z.tar, or the staging directory
# build-release-bundle.sh produces before it packs one) and fails the build
# on any source extension, `.map`, `.git`, internal doc path, or vendor
# tool it finds. This deny-list is a frozen contract (phase-v4-3 brief §4):
# loosening it requires an amendment, never a local exception.
#
# Usage: scripts/lint-release-manifest.sh <bundle-dir>
#
# What "clean" means today, verified by reading build-release-bundle.sh:
# it stages bin/ (stripped Go binaries), www/ (built frontend, no source
# maps — vite.config.ts sets build.sourcemap: false), freeradius/ (config
# + *.pl bridge scripts + the certs/ Makefile/bootstrap/*.cnf — no *.key/
# *.p12 once FR-127 deletes the checked-in ones), nginx/systemd templates,
# scripts/ (five named installer scripts only, never the whole scripts/
# tree — license-tool/ and release-signing/ are never copied), and
# migrations/*.up.sql. Nothing here is a source file of any extension.
set -euo pipefail

BUNDLE_DIR="${1:?usage: lint-release-manifest.sh <bundle-dir>}"
[ -d "$BUNDLE_DIR" ] || { echo "lint-release-manifest: not a directory: $BUNDLE_DIR" >&2; exit 1; }

log() { printf '[lint-release-manifest] %s\n' "$*"; }

# Extensions no shipped bundle may ever contain: HikRAD's own application
# source languages (Go, TypeScript/JSX — the compiled/bundled app, never the
# source behind it), any kind of source map, and languages that never
# legitimately appear anywhere in this stack (defense-in-depth against a
# future accidental inclusion). Deliberately NOT .py/.pl/.sh: FreeRADIUS's
# own stock mods-config/python{,3}/ example files ship as part of the
# deploy/freeradius config tree (cp -r'd whole, by design — see
# build-release-bundle.sh), the RADIUS bridge scripts are Perl, and the
# installer itself is shell — none of those are "leaked source," they are
# this product's deliberately-shipped install/glue layer, same as install.sh
# always has been. Loosening or tightening this list is the frozen-contract
# change the phase brief means — verified against the real bundle, not
# guessed (found this file needed a fix by running it against a real build,
# which flagged FreeRADIUS's own bundled examples as false positives).
DENY_EXTENSIONS_RE='\.(go|ts|tsx|jsx|c|cc|cpp|h|hpp|java|rb|map)$'

# Paths no shipped bundle may ever contain: a .git directory, this repo's
# internal docs, or the vendor-only license-tool/release-signing trees
# (which build-release-bundle.sh never copies, but this is defense-in-depth
# against a future edit that accidentally globs scripts/ wholesale).
DENY_PATHS_RE='(^|/)\.git(/|$)|(^|/)docs(/|$)|(^|/)(license-tool|release-signing)(/|$)'

hits="$(cd "$BUNDLE_DIR" && find . -type f | sed 's#^\./##' | grep -E "$DENY_EXTENSIONS_RE|$DENY_PATHS_RE" || true)"

if [ -n "$hits" ]; then
  echo "RELEASE MANIFEST VIOLATION (FR-126): forbidden path(s) in the bundle:" >&2
  echo "$hits" | sed 's/^/  /' >&2
  exit 1
fi

log "bundle is source-free: $BUNDLE_DIR"

map_count="$(cd "$BUNDLE_DIR" && find . -type f -name '*.map' | wc -l | tr -d ' ')"
if [ "$map_count" != "0" ]; then
  # Unreachable given the grep above already denies *.map, but stated
  # explicitly as its own check because gate item 10 ("find over the built
  # bundle returns zero .map") is worded as its own acceptance item.
  echo "RELEASE MANIFEST VIOLATION (FR-126, gate item 10): $map_count source map(s) found" >&2
  exit 1
fi
log "no source maps: 0 .map files"
