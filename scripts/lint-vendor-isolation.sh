#!/usr/bin/env bash
# lint-vendor-isolation.sh — FR-17 guard (Agent 2, RADIUS & NAS).
#
# The RADIUS core is vendor-neutral: it emits abstract intents (rate_limit,
# address_pool, …) and ONLY the vendor adapter maps them to concrete VSAs. So a
# `Mikrotik-*` attribute literal must never appear in backend Go code outside
# internal/radius/vendor/. (The FreeRADIUS tree — dictionaries, templates and
# scripts/authorize.pl — is the other side of the same adapter boundary and is
# allowed to name VSAs; this lint only covers the Go core.)
#
# Usage: scripts/lint-vendor-isolation.sh   (run from repo root; exits non-zero
# on a violation). Matches the Mikrotik VSA prefix (e.g. Mikrotik-Rate-Limit).
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BACKEND="$ROOT/backend"

# The VSA prefix. `Mikrotik-` (with the trailing hyphen) is how every MikroTik
# vendor attribute name is spelled; the layeh package identifiers (MikrotikRate
# Limit_SetString) have no hyphen and are fine — they live only in vendor/.
PATTERN='Mikrotik-'

# Search all Go files under backend/ except the vendor adapter package.
hits="$(grep -rnE "$PATTERN" "$BACKEND" \
    --include='*.go' \
    --exclude-dir='vendor' 2>/dev/null || true)"

# --exclude-dir=vendor also skips a top-level Go module vendor dir if present;
# that is fine — third-party vendored code is not our core either.

if [ -n "$hits" ]; then
    echo "VENDOR-ISOLATION VIOLATION (FR-17): a Mikrotik VSA literal appears outside internal/radius/vendor/:" >&2
    echo "$hits" >&2
    exit 1
fi

echo "vendor-isolation: no Mikrotik VSA literal outside internal/radius/vendor/"
exit 0
