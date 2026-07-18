#!/usr/bin/env bash
# lint-no-native-controls.sh — FR-94.3 guard (v2 phase 12, C4).
#
# The component-library decision (Tailwind + Radix, since Phase 1) means no
# screen renders browser-native select/checkbox/radio chrome directly — every
# such control goes through frontend/{panel,portal}/src/components/form/. A
# bare native control anywhere else is a regression this script exists to
# catch, same shape and calling convention as scripts/lint-vendor-isolation.sh
# (FR-17's guard): a plain grep, no ESLint-plugin/AST dependency.
#
# Negative scope — exactly these three (matching FR-94.3 and the phase
# brief's contract C4), no broader: a bare `<select`, `type="checkbox"`/
# `type='checkbox'`, `type="radio"`/`type='radio'`. NOT gated: bare
# `<input type="text"/"tel"/"email"/"password"/"search"/"number"/"date">` —
# these already render with only Tailwind chrome, no OS-native popup/tick.
#
# Usage: scripts/lint-no-native-controls.sh [panel|portal]   (both if omitted)
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

APPS="${1:-}"
if [ -z "$APPS" ]; then
  APPS="panel portal"
fi

PATTERN='<select[ />]|type="checkbox"|type='"'"'checkbox'"'"'|type="radio"|type='"'"'radio'"'"''

FAILED=0
for app in $APPS; do
  SRC="$ROOT/frontend/$app/src"
  [ -d "$SRC" ] || continue

  # Exclude the control library's own implementation and every test file —
  # both legitimately contain the real native element Radix renders
  # underneath, or assert on it.
  hits="$(grep -rnE "$PATTERN" "$SRC" \
      --include='*.tsx' \
      --include='*.ts' \
      2>/dev/null \
      | grep -v -E "/components/form/" \
      | grep -v -E "\.test\.tsx?:" \
      || true)"

  if [ -n "$hits" ]; then
    echo "NO-NATIVE-CONTROLS VIOLATION ($app, FR-94.3): a bare native select/checkbox/radio appears outside components/form/:" >&2
    echo "$hits" >&2
    FAILED=1
  else
    echo "no-native-controls ($app): clean"
  fi
done

exit "$FAILED"
