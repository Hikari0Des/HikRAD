#!/usr/bin/env bash
# verify-bundle.sh — checksum + signature verification for a HikRAD release
# bundle (FR-81.3, phase v2-5 C1/C2). Single source of truth for this check:
# install.sh's --bundle mode, `hikrad update --bundle`, and
# scripts/gate-v2-phase-5.sh all call this instead of re-implementing it.
#
# Usage: verify-bundle.sh <extracted-bundle-dir>
#
# Verifies (in this order, so a checksum mismatch is reported before a
# signature failure that would otherwise mask it):
#   1. SHA256SUMS lists every file actually present and every listed file's
#      hash matches (`sha256sum -c`, run with the bundle dir as cwd so the
#      manifest's relative paths resolve).
#   2. SHA256SUMS.sig is a valid Ed25519 signature over SHA256SUMS, verified
#      against the embedded release public key (openssl pkeyutl, -rawin —
#      Ed25519 signs the message directly, no separate digest step).
#
# Exits 0 only if BOTH pass. Never modifies the directory it's given, never
# touches anything outside it — callers (install.sh/hikrad) are responsible
# for verifying a bundle in a scratch/temp extraction before copying
# anything from it into a live install (FR-81.3's "no partial effect").
set -euo pipefail

die() { printf '[verify-bundle] ERROR: %s\n' "$*" >&2; exit 1; }

BUNDLE_DIR="${1:?usage: verify-bundle.sh <extracted-bundle-dir>}"
[ -d "$BUNDLE_DIR" ] || die "not a directory: $BUNDLE_DIR"
[ -f "$BUNDLE_DIR/SHA256SUMS" ] || die "missing SHA256SUMS — not a valid HikRAD release bundle"
[ -f "$BUNDLE_DIR/SHA256SUMS.sig" ] || die "missing SHA256SUMS.sig — not a valid HikRAD release bundle"
command -v sha256sum >/dev/null 2>&1 || die "sha256sum is required but not found on PATH"
command -v openssl >/dev/null 2>&1 || die "openssl is required but not found on PATH"

# RELEASE_PUBLIC_KEY_PEM (dev key — see scripts/release-signing/README.md for
# the pre-first-shipment rotation ritual): the ONLY place this constant may
# live. Do not duplicate it elsewhere — a mismatch between "what signed it"
# and "what verifies it" is the classic way to ship a check that silently
# rejects everything, or worse, silently accepts everything.
RELEASE_PUBLIC_KEY_PEM='-----BEGIN PUBLIC KEY-----
MCowBQYDK2VwAyEA6p9oOzEkPJSqf5M2wLoeCdFHwCHhTJIWRsb6cQg7M9M=
-----END PUBLIC KEY-----'

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
printf '%s\n' "$RELEASE_PUBLIC_KEY_PEM" > "$WORK/pub.pem"

log() { printf '[verify-bundle] %s\n' "$*"; }

log "Checking file integrity (SHA256SUMS)…"
( cd "$BUNDLE_DIR" && sha256sum -c --strict SHA256SUMS ) \
  || die "checksum verification failed — the bundle is corrupt or was tampered with; refusing to use it"

log "Checking release signature (SHA256SUMS.sig)…"
openssl pkeyutl -verify -pubin -inkey "$WORK/pub.pem" -rawin \
  -in "$BUNDLE_DIR/SHA256SUMS" -sigfile "$BUNDLE_DIR/SHA256SUMS.sig" >/dev/null 2>&1 \
  || die "signature verification failed — SHA256SUMS does not match a genuine HikRAD release signature; refusing to use it"

log "Bundle verified OK: $BUNDLE_DIR"
