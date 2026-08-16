#!/usr/bin/env bash
# lint-no-raw-palette.sh — FR-116 gate 2 (v4-1, D3/D4 root cause: the token
# layer was incomplete, which is what invited raw Tailwind palette values in
# the first place). Locks in what was already true when this phase started
# (a raw-palette grep across all 209 .tsx files returned zero): every color
# utility must resolve through the --hik-* token layer, never a stock
# Tailwind hue-and-shade utility (e.g. `bg-red-500`, `border-slate-300`).
#
# Same shape/calling convention as scripts/lint-vendor-isolation.sh and
# scripts/lint-no-native-controls.sh: a plain grep, no AST dependency.
#
# Scope: the 22 named Tailwind default-palette hues × the standard shade
# scale, across the 15 color-bearing utility prefixes. `white`/`black`/
# `transparent`/`current` are Tailwind's always-on structural keywords, not
# "palette" colors, and are intentionally out of scope (same convention the
# v3-1 design system used).
#
# Usage: scripts/lint-no-raw-palette.sh [panel|portal]   (both if omitted)
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

APPS="${1:-}"
if [ -z "$APPS" ]; then
  APPS="panel portal"
fi

HUES='slate|gray|zinc|neutral|stone|red|orange|amber|yellow|lime|green|emerald|teal|cyan|sky|blue|indigo|violet|purple|fuchsia|pink|rose'
SHADES='50|100|200|300|400|500|600|700|800|900|950'
PREFIXES='bg|text|border|ring|divide|fill|stroke|outline|decoration|caret|accent|from|via|to|placeholder'
PATTERN="(${PREFIXES})-(${HUES})-(${SHADES})\\b"

FAILED=0
for app in $APPS; do
  SRC="$ROOT/frontend/$app/src"
  [ -d "$SRC" ] || continue

  hits="$(grep -rnEo "$PATTERN" "$SRC" \
      --include='*.tsx' \
      --include='*.ts' \
      2>/dev/null || true)"

  if [ -n "$hits" ]; then
    echo "NO-RAW-PALETTE VIOLATION ($app, FR-116): a raw Tailwind palette utility appears outside the token layer:" >&2
    echo "$hits" >&2
    FAILED=1
  else
    echo "no-raw-palette ($app): clean"
  fi
done

# The shared package is consumed by both apps (portal's Tailwind content glob
# scans it directly; panel's components could still copy-paste from it), so
# it is held to the same rule regardless of which app's run it rode in on.
SHARED_SRC="$ROOT/frontend/shared/src"
if [ -d "$SHARED_SRC" ]; then
  hits="$(grep -rnEo "$PATTERN" "$SHARED_SRC" --include='*.tsx' --include='*.ts' 2>/dev/null || true)"
  if [ -n "$hits" ]; then
    echo "NO-RAW-PALETTE VIOLATION (shared, FR-116): a raw Tailwind palette utility appears outside the token layer:" >&2
    echo "$hits" >&2
    FAILED=1
  else
    echo "no-raw-palette (shared): clean"
  fi
fi

exit "$FAILED"
