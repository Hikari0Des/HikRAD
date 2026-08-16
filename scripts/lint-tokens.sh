#!/usr/bin/env bash
# lint-tokens.sh — FR-116 gate 1 (v4-1). Entry point matching the calling
# convention of the other frontend lint-*.sh scripts (invoked as
# `sh scripts/lint-tokens.sh [panel|portal]` from each app's package.json);
# the actual check (ground-truth verification against each app's real build
# output, see the file header there) is implemented in lint-tokens.mjs since
# it needs a real parser, not grep.
#
# Usage: scripts/lint-tokens.sh [panel|portal]   (both if omitted)
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

node "$ROOT/scripts/lint-tokens.mjs" "$@"
