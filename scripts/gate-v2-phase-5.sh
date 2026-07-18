#!/bin/sh
# gate-v2-phase-5.sh — machine-checkable legs of the v2 phase-5 integration
# gate (docs/v2/phases/phase-v2-5-closed-source/00-phase.md). Each check
# prints a label followed by PASS or FAIL; exit status is non-zero if any
# leg fails.
#
# Two legs (items 8-9 in the phase doc: clean-VM no-source install, real-
# bundle tamper-refusal end-to-end) are human/hardware legs this script
# cannot exercise — they are recorded as documented-pending in gate-result.md,
# same sanctioned pattern as the v1 Phase 5 gate's restore-round-trip item.
#
# Usage:  sh scripts/gate-v2-phase-5.sh
set -u

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$ROOT" || exit 2

FAILED=0
pass() { printf '  [PASS] %s\n' "$1"; }
fail() { printf '  [FAIL] %s\n' "$1"; FAILED=1; }
check() { # check "<label>" <cmd...>
  label=$1; shift
  if "$@" >/dev/null 2>&1; then pass "$label"; else fail "$label"; fi
}

GO=go
command -v go >/dev/null 2>&1 || {
  for c in "/c/Program Files/Go/bin/go" "/usr/local/go/bin/go"; do
    [ -x "$c" ] && GO=$c && break
  done
}

echo "== v2 phase 5 gate (closed-source distribution & licensing hardening) =="

# --- Item 1: signature round-trip + tamper-refusal -------------------------
echo "-- Release signing mechanism --"
check "openssl available" command -v openssl
WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT
if openssl genpkey -algorithm ED25519 -out "$WORK/priv.pem" >/dev/null 2>&1 \
   && openssl pkey -in "$WORK/priv.pem" -pubout -out "$WORK/pub.pem" >/dev/null 2>&1; then
  pass "openssl Ed25519 keygen (environment supports the signing scheme, C1)"
  printf 'a  fake-file-one\nb  fake-file-two\n' > "$WORK/SHA256SUMS"
  openssl pkeyutl -sign -inkey "$WORK/priv.pem" -rawin -in "$WORK/SHA256SUMS" -out "$WORK/SHA256SUMS.sig" >/dev/null 2>&1
  check "openssl Ed25519 sign+verify round-trip (valid signature accepted)" \
    openssl pkeyutl -verify -pubin -inkey "$WORK/pub.pem" -rawin -in "$WORK/SHA256SUMS" -sigfile "$WORK/SHA256SUMS.sig"
  printf 'a  fake-file-one\nb  TAMPERED\n' > "$WORK/SHA256SUMS"
  check "openssl Ed25519 verify rejects a tampered file (must FAIL to verify -> leg PASSES)" \
    sh -c '! openssl pkeyutl -verify -pubin -inkey "$1/pub.pem" -rawin -in "$1/SHA256SUMS" -sigfile "$1/SHA256SUMS.sig"' _ "$WORK"
else
  fail "openssl Ed25519 keygen (environment does not support the signing scheme, C1)"
fi

echo "-- scripts/verify-bundle.sh (C1/C2, implementation) --"
if [ -f scripts/verify-bundle.sh ]; then
  check "scripts/verify-bundle.sh is syntactically valid" bash -n scripts/verify-bundle.sh
  check "scripts/verify-bundle.sh embeds a PEM public key (not a bare base64 constant)" \
    grep -q "BEGIN PUBLIC KEY" scripts/verify-bundle.sh
else
  fail "scripts/verify-bundle.sh present (not yet implemented)"
fi

# --- Item 2: compose rendering (C5) -----------------------------------------
echo "-- Compose rendering (C5) --"
if [ -f scripts/render-release-compose.sh ]; then
  check "scripts/render-release-compose.sh is syntactically valid" bash -n scripts/render-release-compose.sh
  if HIKRAD_VERSION=v0.0.0-gate HIKRAD_REGISTRY=ghcr.io/hikrad \
       bash scripts/render-release-compose.sh deploy/compose.yml "$WORK/rendered-compose.yml" >/dev/null 2>&1; then
    check "rendered compose.yml has no build: for the 4 HikRAD services" \
      sh -c '! grep -q "build:" "$1"' _ "$WORK/rendered-compose.yml"
    check "rendered compose.yml still tags all 4 HikRAD services with image:" \
      sh -c 'grep -c "ghcr.io/hikrad/" "$1" | grep -q "^4$"' _ "$WORK/rendered-compose.yml"
    check "third-party services (postgres/redis/freeradius) unchanged" \
      sh -c 'grep -q "timescale/timescaledb" "$1" && grep -q "redis:7-alpine" "$1" && grep -q "freeradius/freeradius-server" "$1"' _ "$WORK/rendered-compose.yml"
  else
    fail "scripts/render-release-compose.sh runs against deploy/compose.yml"
  fi
else
  fail "scripts/render-release-compose.sh present (not yet implemented)"
fi

# --- Item 3: no new blocking license path (C6, hard boundary) --------------
echo "-- License boot verification is informational-only (C6) --"
for svc in hikrad-acct hikrad-monitor; do
  f="backend/cmd/$svc/main.go"
  if [ -f "$f" ]; then
    check "$svc calls platform.RefreshLicenseCache at boot" grep -q "RefreshLicenseCache" "$f"
    check "$svc starts a re-verify ticker" grep -q "time.NewTicker" "$f"
    check "$svc has no license-state-conditioned exit/return-from-main (must find NONE -> leg PASSES)" \
      sh -c '! grep -nE "(license\.State|CachedLicenseState)" "$1" | grep -iE "exit|os\.Exit|return err"' _ "$f"
  else
    fail "$f present"
  fi
done

# --- Item 4: licenseGate scope unchanged ------------------------------------
echo "-- licenseGate regression --"
check_go_test() {
  label=$1; shift
  out=$("$@" -v 2>&1)
  case "$out" in
    *FAIL*) fail "$label" ;;
    *PASS*) pass "$label" ;;
    *)      fail "$label" ;;
  esac
}
check_go_test "[leg] internal/httpapi license_gate suite unchanged" \
  "$GO" -C backend test ./internal/httpapi/... -run TestLicenseGate

# --- Item 5: dev-mode regression (pre-existing CI legs) ---------------------
echo "-- Dev-mode / build health --"
check "backend builds" "$GO" -C backend build ./...
check "backend vets" "$GO" -C backend vet ./...
check "install.sh still syntactically valid" bash -n scripts/install.sh
check "hikrad still syntactically valid" bash -n scripts/hikrad
check "gen-env self-test (unchanged source-build path)" bash scripts/gen-env.test.sh

# --- Item 6: bundle-mode installer plumbing (C4) ----------------------------
echo "-- Installer bundle mode (C4) --"
check "install.sh references --bundle" grep -q -- "--bundle" scripts/install.sh
check "hikrad's cmd_update verifies before loading (calls verify-bundle.sh, not a bare docker load)" \
  grep -q "verify-bundle" scripts/hikrad
check "install.meta template/writer includes HIKRAD_DELIVERY_MODE" \
  grep -q "HIKRAD_DELIVERY_MODE" scripts/install.sh

# --- Item 7: docs accuracy ---------------------------------------------------
echo "-- Docs --"
check "PRD carries FR-81..FR-83" \
  sh -c 'grep -q "FR-81" docs/PRD.md && grep -q "FR-82" docs/PRD.md && grep -q "FR-83" docs/PRD.md'
check "phase brief present" test -f docs/v2/phases/phase-v2-5-closed-source/00-phase.md
check "release-checklist.md carries the signing & registry section" \
  grep -q "Signing & registry" docs/ops/release-checklist.md
check "install-guide.md mentions bundle mode" grep -qi "bundle" docs/ops/install-guide.md
check "update.md mentions signed/verified bundles" grep -qi "sign" docs/ops/update.md
check "known-issues.md updated during this phase" grep -q "2026-07-18" docs/ops/known-issues.md

echo
echo "== Human/hardware legs (not scriptable — record status in gate-result.md) =="
echo "  [ ] Clean-VM no-source install (AC-81a): real Ubuntu 22.04/24.04 VM, no Go toolchain,"
echo "      no HikRAD checkout, install.sh --bundle <signed bundle> -> healthy stack + PPPoE Access-Accept."
echo "  [ ] Real-bundle tamper-refusal end-to-end: same VM, one byte flipped in a real multi-GB"
echo "      bundle, install.sh/hikrad update refuse before touching the live install."

echo
if [ "$FAILED" -eq 0 ]; then
  echo "== v2 phase 5 gate: ALL SCRIPTED LEGS PASSED =="
else
  echo "== v2 phase 5 gate: FAILURES ABOVE =="
fi
exit "$FAILED"
