#!/usr/bin/env bash
# render-release-compose.sh — turns the dev deploy/compose.yml (which builds
# the 4 HikRAD images from source) into a release bundle's compose.yml (which
# pulls them by tag from the registry instead). Phase v2-5, C5.
#
# Usage: render-release-compose.sh <input-compose.yml> <output-compose.yml>
# Env:   HIKRAD_VERSION   required — the tag to pin (e.g. v1.2.0)
#        HIKRAD_REGISTRY  default: ghcr.io/hikrad
#
# Only the 4 HikRAD-built services' `build:` stanza is replaced with a single
# `image: <registry>/<image>:<version>` line at the same indentation; every
# other line (postgres, redis, freeradius, caddy's non-build keys,
# cloudflared, healthchecks, volumes, ports, deploy.resources, comments) is
# passed through byte-identical. This is intentionally a targeted text
# transform, not a YAML rewrite — deploy/compose.yml is hand-authored and
# consistently indented (2-space service keys, 4-space service-body keys),
# and a full YAML round-trip would risk reformatting comments/spacing that
# matter to readers of the shipped file.
set -euo pipefail

IN="${1:?usage: render-release-compose.sh <input-compose.yml> <output-compose.yml>}"
OUT="${2:?usage: render-release-compose.sh <input-compose.yml> <output-compose.yml>}"
[ -f "$IN" ] || { echo "render-release-compose.sh: input not found: $IN" >&2; exit 1; }
: "${HIKRAD_VERSION:?HIKRAD_VERSION is required (e.g. v1.2.0)}"
HIKRAD_REGISTRY="${HIKRAD_REGISTRY:-ghcr.io/hikrad}"

# service compose-key -> pushed image name (C3: registry naming). caddy's
# compose service is named "caddy" but its pushed image is "hikrad-caddy" to
# avoid colliding with the public upstream "caddy" image name on any registry.
awk -v version="$HIKRAD_VERSION" -v registry="$HIKRAD_REGISTRY" '
  BEGIN {
    image["hikrad-api"] = "hikrad-api"
    image["hikrad-acct"] = "hikrad-acct"
    image["hikrad-monitor"] = "hikrad-monitor"
    image["caddy"] = "hikrad-caddy"
    skipping = 0
    current_service = ""
  }
  {
    line = $0
    # Detect a top-level service key: exactly 2 leading spaces, ends with ":".
    if (match(line, /^  [A-Za-z0-9_-]+:[[:space:]]*$/)) {
      current_service = line
      sub(/^  /, "", current_service)
      sub(/:[[:space:]]*$/, "", current_service)
      skipping = 0
    }

    if (skipping) {
      # Still inside the build: sub-block if this line is more indented than
      # 4 spaces (i.e. indent >= 6) or blank. A line at indent <= 4 that is
      # non-blank ends the block — emit the replacement first, then fall
      # through to print this new line normally.
      if (match(line, /^      /) || line ~ /^[[:space:]]*$/) {
        next
      }
      printf "    image: %s/%s:%s\n", registry, image[current_service], version
      skipping = 0
    }

    if ((current_service in image) && match(line, /^    build:[[:space:]]*$/)) {
      skipping = 1
      next
    }

    print line
  }
  END {
    if (skipping) {
      printf "    image: %s/%s:%s\n", registry, image[current_service], version
    }
  }
' "$IN" > "$OUT"

echo "render-release-compose.sh: wrote $OUT (version=$HIKRAD_VERSION registry=$HIKRAD_REGISTRY)"
