#!/bin/sh
# gate-v2-phase-10.sh — machine-checkable legs of the v2 phase-10 integration
# gate (docs/v2/phases/phase-v2-10-custom-dashboards/00-phase.md). Each check
# prints a label followed by PASS or FAIL; exit status is non-zero if any leg
# fails. No human/hardware legs — this phase has no router/device dependency.
#
# Usage:  sh scripts/gate-v2-phase-10.sh
#   Set HIKRAD_TEST_DB_URL (+ HIKRAD_TEST_REDIS_URL) to exercise the DB-gated
#   legs (permission gating, forbidden-widget absence, cross-device layout,
#   default-equals-today, reset-to-default, validation, backward
#   compatibility). Without them the DB-gated Go legs self-skip — and this
#   script reports them FAIL, on purpose: skipped is not passed for a gate
#   whose point is proving the behavior.
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
# check_go_test: `go test` prints the summary word "PASS" in THREE cases that
# must not be confused: a real pass, a self-skip (t.Skip on a missing
# HIKRAD_TEST_DB_URL), and — the one that matters most for a freshly-written
# gate script naming tests that don't exist yet — a -run pattern that matches
# NO test at all ("warning: no tests to run" + a bare summary "PASS", exit 0).
# Require an explicit "--- PASS: <name>" line and reject any "--- SKIP:"/
# "--- FAIL:" line, so a leg can only go green by a named test actually
# running and passing.
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

echo "== v2 phase 10 gate (customizable per-manager dashboards) =="

# --- Item 1: no schema change -----------------------------------------------
echo "-- No schema change --"
check "no migration above 0590 exists (dashboard_layout is an additive JSON key, not a column — this phase issues no migration)" \
  sh -c '! ls backend/migrations/059[1-9]_*.sql backend/migrations/06[0-9][0-9]_*.sql 2>/dev/null | grep -q .'
check "backend builds" "$GO" -C backend build ./...
check "backend vets" "$GO" -C backend vet ./...
check "vendor isolation grep green (FR-17, unaffected by this phase)" sh scripts/lint-vendor-isolation.sh

# --- Item 2: widget catalog permission gating (DB-gated, FR-89.1) ----------
echo "-- Permission gating (DB-gated) --"
check_go_test "[leg] the builtin agent role's actual permission set sees subs/revenue/my-balance but not online-now/pipeline/nas-health/alerts-feed/pending-tickets" \
  "$GO" -C backend test ./internal/monitorsvc/... -run TestDashboardWidgetsPermissionGating

# --- Item 3: forbidden widget absent, not erroring (DB-gated, FR-89.3) ----
echo "-- Forbidden widget absence (DB-gated) --"
check_go_test "[leg] a forbidden or unknown widget id in ?widgets= never 400s/403s the call; the key is simply missing from a 200" \
  "$GO" -C backend test ./internal/monitorsvc/... -run TestDashboardForbiddenWidgetAbsent

# --- Item 4: cross-device layout (DB-gated, mirrors v2-6 AC-84a) ----------
echo "-- Cross-device layout (DB-gated) --"
check_go_test "[leg] a PUT'd dashboard_layout is visible on an independent, later GET (simulated second device)" \
  "$GO" -C backend test ./internal/auth/... -run TestPreferencesDashboardLayoutCrossDeviceSeed

# --- Item 5: default-equals-today snapshot (DB-gated, FR-90.1) ------------
echo "-- Default-equals-today snapshot (DB-gated) --"
check_go_test "[leg] a manager with no stored layout gets the exact permission-filtered widget set the pre-phase full aggregate already returns" \
  "$GO" -C backend test ./internal/monitorsvc/... -run TestDashboardDefaultEqualsToday

# --- Item 6: reset-to-default (DB-gated) -----------------------------------
echo "-- Reset-to-default (DB-gated) --"
check_go_test "[leg] PUT /me/preferences with dashboard_layout omitted clears a previously-saved layout back to default" \
  "$GO" -C backend test ./internal/auth/... -run TestPreferencesDashboardLayoutResetToDefault

# --- Item 7: validation (DB-gated) -----------------------------------------
echo "-- Layout validation (DB-gated) --"
check_go_test "[leg] an unknown widget id or invalid size 422s naming the offending path; nothing is written" \
  "$GO" -C backend test ./internal/auth/... -run TestPreferencesDashboardLayoutValidation

# --- Item 8: backward compatibility (DB-gated) -----------------------------
echo "-- Backward compatibility (DB-gated) --"
check_go_test "[leg] GET /api/v1/dashboard with no ?widgets= is byte-for-byte unchanged from the pre-phase response shape" \
  "$GO" -C backend test ./internal/monitorsvc/... -run TestDashboardBackwardCompatibleNoWidgetsParam

# --- Item 9: phone-first single column (panel) -----------------------------
echo "-- Phone-first single column --"
check "panel dashboard mobile-single-column test present and green" \
  sh -c 'cd frontend/panel && npx vitest run src/pages/DashboardPage.test.tsx'

# --- Item 10: full regression ------------------------------------------------
# internal/monitorsvc has no pre-existing DB-gated HTTP-endpoint suite (its
# existing tests are all unit-level: quiet hours, cooldown, dispatcher, SNMP
# encoding, downtime detection, WhatsApp templates) — this phase's own new
# DB-gated tests (items 2/3/5/8 above) are the first ones to exercise
# dashboard_api.go end to end, so "regression" here means the full unit suite
# stays green, not a pre-existing endpoint suite that doesn't exist yet.
echo "-- Full monitorsvc unit regression --"
check "internal/monitorsvc unit suite green (unaffected by this phase's endpoint changes)" \
  "$GO" -C backend test ./internal/monitorsvc/...

# --- Item 11: panel/portal ---------------------------------------------------
echo "-- Panel/portal --"
check "panel dashboard widget registry + edit mode present" \
  sh -c 'grep -rq "dashboard_layout" frontend/panel/src'
check "shared build"  sh -c 'cd frontend && npm run build --workspace=@hikrad/shared'
check "panel build"   sh -c 'cd frontend/panel && npm run build'
check "panel lint"    sh -c 'cd frontend/panel && npm run lint'
check "panel vitest"  sh -c 'cd frontend/panel && npx vitest run'
check "i18n:check (0 missing keys / 0 hardcoded strings)" sh -c 'cd frontend && node shared/scripts/i18n-check.mjs'

# --- Item 12: docs accuracy --------------------------------------------------
echo "-- Docs --"
check "PRD carries FR-89 and FR-90" \
  sh -c 'grep -q "FR-89" docs/PRD.md && grep -q "FR-90" docs/PRD.md'
check "sub-PRD 03 carries FR-89/FR-90" \
  sh -c 'grep -q "FR-89" docs/prd/03-lossless-accounting-live-monitoring.md && grep -q "FR-90" docs/prd/03-lossless-accounting-live-monitoring.md'
check "phase brief present" test -f docs/v2/phases/phase-v2-10-custom-dashboards/00-phase.md

echo
if [ "$FAILED" -eq 0 ]; then
  echo "== v2 phase 10 gate: ALL SCRIPTED LEGS PASSED =="
else
  echo "== v2 phase 10 gate: FAILURES ABOVE =="
fi
exit "$FAILED"
