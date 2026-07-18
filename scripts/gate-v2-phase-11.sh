#!/bin/sh
# gate-v2-phase-11.sh — machine-checkable legs of the v2 phase-11 integration
# gate (docs/v2/phases/phase-v2-11-instance-branding/00-phase.md). Each check
# prints a label followed by PASS or FAIL; exit status is non-zero if any leg
# fails. No human/hardware legs — this phase has no router/device dependency.
#
# Usage:  sh scripts/gate-v2-phase-11.sh
#   Set HIKRAD_TEST_DB_URL (+ HIKRAD_TEST_REDIS_URL) to exercise the DB-gated
#   legs (public endpoint fix, Hotspot package fix, receipt fix, upload
#   validation, logo removal, generic-PUT rejection, audit). Without them the
#   DB-gated Go legs self-skip — and this script reports them FAIL, on
#   purpose: skipped is not passed for a gate whose entire point is proving
#   three previously-silent bugs are actually fixed.
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
# check_go_test: see gate-v2-phase-10.sh for the full rationale — a bare
# summary "PASS" from `go test -run <pattern>` is printed even when the
# pattern matched zero tests, so require a literal "--- PASS: <name>" line
# and treat "--- SKIP:"/"--- FAIL:" as failing.
check_go_test() {
  label=$1; shift
  out=$("$@" -v 2>&1)
  case "$out" in
    *'--- SKIP:'*) fail "$label (SKIPPED — set HIKRAD_TEST_DB_URL to run this leg for real)" ;;
    *'--- FAIL:'*) fail "$label" ;;
    *'--- PASS:'*) pass "$label" ;;
    *)             fail "$label (no matching test ran — check the test name)" ;;
  esac
}

GO=go
command -v go >/dev/null 2>&1 || {
  for c in "/c/Program Files/Go/bin/go" "/usr/local/go/bin/go"; do
    [ -x "$c" ] && GO=$c && break
  done
}

echo "== v2 phase 11 gate (instance branding) =="

# --- Item 1: no schema change -----------------------------------------------
echo "-- No schema change --"
check "no migration above 0590 exists (this phase corrects a read pattern + adds disk storage, not a schema change)" \
  sh -c '! ls backend/migrations/0591_*.sql backend/migrations/059[2-9]_*.sql backend/migrations/06[0-9][0-9]_*.sql 2>/dev/null | grep -q .'
check "backend builds" "$GO" -C backend build ./...
check "backend vets" "$GO" -C backend vet ./...
check "vendor isolation grep green (FR-17, unaffected by this phase)" sh scripts/lint-vendor-isolation.sh

# --- Item 2: public endpoint bug 1/2 fixed (DB-gated) -----------------------
echo "-- Public GET /api/v1/branding reflects real settings (DB-gated) --"
check_go_test "[leg] PUT settings/branding then unauthenticated GET /branding returns the configured name/color, not the hardcoded default" \
  "$GO" -C backend test ./internal/portalapi/... -run TestBrandingEndpointReflectsConfiguredIdentity

# --- Item 3: Hotspot package bug 1/2 fixed (DB-gated) -----------------------
echo "-- Hotspot captive-portal package reflects real branding (DB-gated) --"
check_go_test "[leg] generated login.html contains the configured name and an inlined data: URI logo, not a fetchable URL" \
  "$GO" -C backend test ./internal/radius/... -run TestHotspotPackageEmbedsConfiguredBranding

# --- Item 4: receipt bug 3 fixed (DB-gated) ---------------------------------
echo "-- Receipt header respects the boolean toggle (DB-gated) --"
check_go_test "[leg] receipt_branding=true shows the configured name; =false shows the generic literal regardless" \
  "$GO" -C backend test ./internal/billing/... -run TestReceiptBrandingBooleanRespected

# --- Item 5: upload validation (DB-gated) -----------------------------------
echo "-- Logo upload validation (DB-gated) --"
check_go_test "[leg] oversized/wrong-type/script-bearing-SVG uploads 422; valid PNG/SVG round-trip byte-identical via GET /branding/logo" \
  "$GO" -C backend test ./internal/platform/... -run TestStoreLogoValidation

# --- Item 6: logo removal (DB-gated) ----------------------------------------
echo "-- Logo removal falls back cleanly (DB-gated) --"
check_go_test "[leg] DELETE .../branding/logo clears logo_url to null; GET /branding/logo then 404s" \
  "$GO" -C backend test ./internal/platform/... -run TestDeleteLogoFallsBackCleanly

# --- Item 7: logo_url rejected on generic group PUT (DB-gated) -------------
echo "-- logo_url rejected on the generic settings PUT (DB-gated) --"
check_go_test "[leg] PUT /api/v1/settings/branding with logo_url in the body 422s naming that field; stored value unchanged" \
  "$GO" -C backend test ./internal/platform/setupapi/... -run TestBrandingGroupPutRejectsLogoURL

# --- Item 8: audit (DB-gated) ------------------------------------------------
echo "-- Audit log (DB-gated) --"
check_go_test "[leg] a logo upload and a name change each produce a settings.update audit_log row" \
  "$GO" -C backend test ./internal/platform/setupapi/... -run TestBrandingChangesAudited

# --- Item 9: panel threading -------------------------------------------------
echo "-- Panel threading --"
check "panel sidebar/login/title branding test present and green" \
  sh -c 'cd frontend/panel && npx vitest run src/branding.test.tsx'

# --- Item 10: offline / no external fetch (grep) ----------------------------
echo "-- Offline / no external fetch --"
check "internal/platform/branding.go makes no outbound HTTP call" \
  sh -c '! grep -E "http\.(Get|Post|NewRequest|Client\{)" backend/internal/platform/branding.go 2>/dev/null | grep -q .'
check "Hotspot builder makes no outbound HTTP call for branding" \
  sh -c '! grep -E "http\.(Get|Post)\(" backend/internal/radius/hotspot.go 2>/dev/null | grep -q .'
check "receipt renderer makes no outbound HTTP call" \
  sh -c '! grep -E "http\.(Get|Post)\(" backend/internal/billing/receipt.go 2>/dev/null | grep -q .'

# --- Item 11: portal + PWA regression ---------------------------------------
echo "-- Portal + PWA regression --"
check "portal branded-login test green" \
  sh -c 'cd frontend/portal && npx vitest run src/pages/LoginPage.test.tsx 2>/dev/null || npx vitest run src/test/portal.test.tsx'
check "panel BrandedManifestLink test green" \
  sh -c 'cd frontend/panel && npx vitest run src/pwa/BrandedManifestLink.test.tsx'
check "portal BrandedManifestLink test green" \
  sh -c 'cd frontend/portal && npx vitest run src/pwa/BrandedManifestLink.test.tsx'

# --- Item 12: panel/portal build/lint/i18n -----------------------------------
echo "-- Panel/portal build/lint/i18n --"
check "shared build"  sh -c 'cd frontend && npm run build --workspace=@hikrad/shared'
check "panel build"   sh -c 'cd frontend/panel && npm run build'
check "panel lint"    sh -c 'cd frontend/panel && npm run lint'
check "portal build"  sh -c 'cd frontend/portal && npm run build'
check "portal lint"   sh -c 'cd frontend/portal && npm run lint'
check "i18n:check (0 missing keys / 0 hardcoded strings)" sh -c 'cd frontend && node shared/scripts/i18n-check.mjs'

# --- Item 13: full regression -------------------------------------------------
echo "-- Full regression --"
check "internal/platform unit+DB suite green" "$GO" -C backend test ./internal/platform/...
check "internal/portalapi unit+DB suite green" "$GO" -C backend test ./internal/portalapi/...
check "internal/radius unit+DB suite green" "$GO" -C backend test ./internal/radius/...
check "internal/billing unit+DB suite green" "$GO" -C backend test ./internal/billing/...

# --- Item 14: docs accuracy --------------------------------------------------
echo "-- Docs --"
check "PRD carries FR-91 and FR-92" \
  sh -c 'grep -q "FR-91" docs/PRD.md && grep -q "FR-92" docs/PRD.md'
check "sub-PRD 01 carries FR-91" \
  sh -c 'grep -q "FR-91" docs/prd/01-platform-install-licensing.md'
check "sub-PRD 07 carries FR-92" \
  sh -c 'grep -q "FR-92" docs/prd/07-subscriber-portal-pwa.md'
check "sub-PRD 08 references the branding endpoint" \
  sh -c 'grep -q "branding" docs/prd/08-reports.md'
check "known-issues.md branding row present" \
  sh -c 'grep -q "internal/portalapi/branding.go" docs/ops/known-issues.md'
check "phase brief present" test -f docs/v2/phases/phase-v2-11-instance-branding/00-phase.md

echo
if [ "$FAILED" -eq 0 ]; then
  echo "== v2 phase 11 gate: ALL SCRIPTED LEGS PASSED =="
else
  echo "== v2 phase 11 gate: FAILURES ABOVE =="
fi
exit "$FAILED"
