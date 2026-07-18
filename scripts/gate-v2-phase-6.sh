#!/bin/sh
# gate-v2-phase-6.sh — machine-checkable legs of the v2 phase-6 integration
# gate (docs/v2/phases/phase-v2-6-preferences/00-phase.md). Each check prints
# a label followed by PASS or FAIL; exit status is non-zero if any leg fails.
# No human/hardware legs — this phase has no router/device dependency.
#
# Usage:  sh scripts/gate-v2-phase-6.sh
#   Set HIKRAD_TEST_DB_URL (+ HIKRAD_TEST_REDIS_URL) to exercise the DB-gated
#   legs (preferences seeding/isolation/validation, email validation, CSV
#   mapping). Without them the DB-gated Go legs self-skip — and this script
#   reports them FAIL, on purpose: skipped is not passed for a gate whose
#   point is proving the behavior.
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
# check_go_test: `go test` exits 0 both when a test passes AND when it
# self-skips (t.Skip on a missing HIKRAD_TEST_DB_URL); require PASS with no
# SKIP so a DB-gated leg cannot report PASS without actually running.
check_go_test() {
  label=$1; shift
  out=$("$@" -v 2>&1)
  case "$out" in
    *SKIP*) fail "$label (SKIPPED — set HIKRAD_TEST_DB_URL to run this leg for real)" ;;
    *PASS*) pass "$label" ;;
    *)      fail "$label" ;;
  esac
}

GO=go
command -v go >/dev/null 2>&1 || {
  for c in "/c/Program Files/Go/bin/go" "/usr/local/go/bin/go"; do
    [ -x "$c" ] && GO=$c && break
  done
}

echo "== v2 phase 6 gate (per-manager preferences + subscriber email) =="

# --- Item 1: schema & migration ---------------------------------------------
echo "-- Schema & migration --"
for f in 0589_manager_preferences 0590_subscribers_email; do
  check "migration ${f} present" test -f "backend/migrations/${f}.up.sql"
done
check "no .down.sql added (FR-51.4 forward-only)" \
  sh -c '! ls backend/migrations/058*.up.sql backend/migrations/059*.up.sql backend/migrations/058*.down.sql backend/migrations/059*.down.sql 2>/dev/null | grep -q "\.down\.sql$"'
check "backend builds" "$GO" -C backend build ./...
check "backend vets" "$GO" -C backend vet ./...
check "vendor isolation grep green (FR-17, unaffected by this phase)" sh scripts/lint-vendor-isolation.sh

# --- Item 2: no-row default is 200, not 404 (DB-gated, C1/C2) --------------
echo "-- No-row default (DB-gated) --"
check_go_test "[leg] a manager with no manager_preferences row gets 200 with zero-value fields, never 404" \
  "$GO" -C backend test ./internal/auth/... -run TestPreferencesNoRowDefaultsToZeroValue

# --- Item 3: cross-device seed test (DB-gated, AC-84a) ----------------------
echo "-- Cross-device seed (DB-gated) --"
check_go_test "[leg] a PUT'd preference is visible on an independent, later GET (simulated second device)" \
  "$GO" -C backend test ./internal/auth/... -run TestPreferencesCrossDeviceSeed

# --- Item 4: cross-manager isolation (DB-gated, AC-84b) ---------------------
echo "-- Cross-manager isolation (DB-gated) --"
check_go_test "[leg] manager B's GET never reflects manager A's PUT; a spoofed manager_id in the body is ignored" \
  "$GO" -C backend test ./internal/auth/... -run TestPreferencesCrossManagerIsolation

# --- Item 5: presentation-only boundary never crossed (FR-84.3) -------------
echo "-- Presentation-only boundary (grep) --"
check "no permission/ScopeFilter/money code path reads manager_preferences" \
  sh -c '! grep -rniE "manager_preferences|notification_prefs" backend/internal/radius backend/internal/billing 2>/dev/null'
check "auth.Can / ScopeFilter implementations do not reference Preferences" \
  sh -c '! grep -nE "func \(m \*Manager\) Can|func ScopeFilter" -A 20 backend/internal/auth/middleware.go backend/internal/auth/roles.go 2>/dev/null | grep -iE "preferences|notification_prefs"'

# --- Item 6: preferences validation (DB-gated, C3) --------------------------
echo "-- Preferences validation (DB-gated) --"
check_go_test "[leg] invalid theme/language/numerals/table_page_size/notification key all 422 with field_errors; nothing written" \
  "$GO" -C backend test ./internal/auth/... -run TestPreferencesValidationRejectsBadInput

# --- Item 7: email validation (DB-gated, C4) --------------------------------
echo "-- Subscriber email validation (DB-gated) --"
check_go_test "[leg] valid email persists and round-trips; malformed email 422s and writes nothing" \
  "$GO" -C backend test ./internal/subscribers/... -run TestSubscriberEmailValidation

# --- Item 8: CSV import mapping (DB-gated, AC-85b, C5) ----------------------
echo "-- CSV import email mapping (DB-gated) --"
check_go_test "[leg] sas4 preset maps an Email header case-insensitively; malformed emails are per-row dry-run errors, zero rows written until corrected" \
  "$GO" -C backend test ./internal/importer/... -run TestImportMapsEmailColumn

# --- Item 9: full regression (DB-gated) -------------------------------------
echo "-- Full auth+subscribers+importer regression (DB-gated) --"
check_go_test "[leg] pre-existing internal/auth DB-gated suite (login/refresh/lockout unaffected)" \
  "$GO" -C backend test ./internal/auth/... -run "TestLogin|TestRefresh|TestLockout|TestTOTP|TestAuditWrites"
check_go_test "[leg] pre-existing internal/subscribers DB-gated suite (CRUD/search/bulk unaffected)" \
  "$GO" -C backend test ./internal/subscribers/... -run "TestCreateSubscriber|TestUpdateSubscriber|TestSearch|TestBulk"
check_go_test "[leg] pre-existing internal/importer DB-gated suite (dry-run/import unaffected)" \
  "$GO" -C backend test ./internal/importer/... -run "TestDryRun|TestImport"

# --- Item 10: panel/portal ---------------------------------------------------
echo "-- Panel/portal --"
check "panel preferences screen present and wired" \
  sh -c 'test -f frontend/panel/src/pages/preferences/MyPreferencesPage.tsx && grep -rq "me/preferences" frontend/panel/src'
check "panel subscriber form/list reference email" \
  sh -c 'grep -rlq "email" frontend/panel/src/pages/subscribers/*.tsx'
check "portal self-edit references email" \
  sh -c 'grep -rlq "email" frontend/portal/src'
check "shared build"  sh -c 'cd frontend && npm run build --workspace=@hikrad/shared'
check "panel build"   sh -c 'cd frontend/panel && npm run build'
check "panel lint"    sh -c 'cd frontend/panel && npm run lint'
check "panel vitest"  sh -c 'cd frontend/panel && npx vitest run'
check "portal build"  sh -c 'cd frontend/portal && npm run build'
check "i18n:check (0 missing keys / 0 hardcoded strings)" sh -c 'cd frontend && node shared/scripts/i18n-check.mjs'

# --- Item 11: docs accuracy --------------------------------------------------
echo "-- Docs --"
check "PRD carries FR-84 and FR-85" \
  sh -c 'grep -q "FR-84" docs/PRD.md && grep -q "FR-85" docs/PRD.md'
check "sub-PRD 01 carries FR-84" grep -q "FR-84" docs/prd/01-platform-install-licensing.md
check "sub-PRD 04 carries FR-85" grep -q "FR-85" docs/prd/04-subscribers-profiles.md
check "phase brief present" test -f docs/v2/phases/phase-v2-6-preferences/00-phase.md

echo
if [ "$FAILED" -eq 0 ]; then
  echo "== v2 phase 6 gate: ALL SCRIPTED LEGS PASSED =="
else
  echo "== v2 phase 6 gate: FAILURES ABOVE =="
fi
exit "$FAILED"
