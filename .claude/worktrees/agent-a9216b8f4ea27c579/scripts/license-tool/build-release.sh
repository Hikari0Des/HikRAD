#!/usr/bin/env bash
# Cross-compile the license issuer into standalone binaries for machines that
# do NOT have Go installed. Go binaries are statically linked, so the resulting
# file is the only thing the vendor needs — copy it to a Windows/Mac/Linux box
# and run it directly (no Go, no dependencies).
#
#   ./build-release.sh            # builds all targets into ./dist/
#
# Then, on the target machine, run the GUI issuer once and it opens a browser:
#   Windows:  license-tool-windows-amd64.exe serve -key vendor-key.b64 -keyid K-2026-0001
#   macOS:    ./license-tool-macos-arm64 serve -key vendor-key.b64 -keyid K-2026-0001
#   Linux:    ./license-tool-linux-amd64 serve -key vendor-key.b64 -keyid K-2026-0001
#
# The private key file travels with the operator (never committed); the binary
# is inert without it.
set -euo pipefail
cd "$(dirname "$0")"

OUT="dist"
mkdir -p "$OUT"
VERSION="$(git -C . describe --tags --always 2>/dev/null || echo dev)"
echo "Building license-tool ($VERSION) → $OUT/"

# GOOS/GOARCH/extension triples. Add rows as needed.
targets=(
  "windows amd64 .exe"
  "windows arm64 .exe"
  "darwin  amd64 ''"
  "darwin  arm64 ''"
  "linux   amd64 ''"
  "linux   arm64 ''"
)

for t in "${targets[@]}"; do
  # shellcheck disable=SC2086
  set -- $t
  goos="$1"; goarch="$2"; ext="${3//\'/}"
  out="$OUT/license-tool-${goos/darwin/macos}-${goarch}${ext}"
  CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" \
    go build -trimpath -ldflags="-s -w" -o "$out" .
  echo "  $out"
done

echo "Done. Ship a single binary + the vendor key file to the issuing machine."
