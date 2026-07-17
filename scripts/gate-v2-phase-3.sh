#!/bin/sh
# gate-v2-phase-2.sh — machine-checkable legs of the v2 phase-2 integration
# gate (docs/v2/phases/phase-v2-3-autosetup-config-manager/00-phase.md). Each
# check prints a label followed by PASS or FAIL; exit status is non-zero if
# any leg fails. Human/hardware legs (creating a real hotspot zone end-to-end,
# adopting a real pre-existing PPPoE server, an update resolution against a
# real router) are documented-pending in gate-result.md and NOT scripted here.
#
# Usage:  sh scripts/gate-v2-phase-2.sh
#   Set HIKRAD_TEST_DB_URL (+ HIKRAD_TEST_REDIS_URL) to exercise the DB-gated
#   legs (config inspection, service create/edit/adopt HTTP tests). Without
#   them the DB-gated Go legs self-skip — and this script reports them FAIL,
#   on purpose: skipped is not passed for a gate whose point is proving the
#   behavior.
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

echo "== v2 phase 3 gate (NAS auto-setup config manager + server management) =="

# --- Schema & migration (item 1) --------------------------------------------
echo "-- Schema & migration --"
check "migration 0520_nas_services_management_mode present" \
  test -f backend/migrations/0520_nas_services_management_mode.up.sql
check "0520 adds management_mode with a router default" \
  grep -Eq "management_mode.*DEFAULT 'router'" backend/migrations/0520_nas_services_management_mode.up.sql
check "no .down.sql added (FR-51.4 forward-only)" \
  sh -c '! ls backend/migrations/ | grep -q "\.down\.sql$"'

# --- C1 non-invalidation + build (item 2) -----------------------------------
echo "-- Backend build + non-invalidation --"
check "backend builds" "$GO" -C backend build ./...
check "backend vets" "$GO" -C backend vet ./...
check "vendor isolation grep green (FR-17)" sh scripts/lint-vendor-isolation.sh
check "no /remove sentence in auto-setup or service-provisioning files" \
  sh -c '! grep -rn "\"/[a-z/]*remove\"" backend/internal/radius/vendor/mikrotik_autosetup.go backend/internal/radius/vendor/mikrotik_service_provision.go'
check_go_test "[leg] full vendor auto-setup suite passes with resolutions=nil (C1)" \
  "$GO" -C backend test ./internal/radius/vendor/... -run TestPlanAutoSetup_FreshRouter_AllAdditive

# --- FR-65 config inspection (fake-router unit legs) ------------------------
echo "-- FR-65 config inspection --"
check_go_test "[leg] ReadConfig reflects planted router state" \
  "$GO" -C backend test ./internal/radius/vendor/... -run TestReadConfig_ReflectsPlantedRouterState
check_go_test "[leg] ReadConfig never exposes a secret value" \
  "$GO" -C backend test ./internal/radius/vendor/... -run TestReadConfig_NeverExposesSecretValue

# --- FR-66 form-driven + modify-or-create (fake-router unit legs, item 5) --
echo "-- FR-66 resolutions --"
check_go_test "[leg] update resolution produces a /set item, drops the conflict" \
  "$GO" -C backend test ./internal/radius/vendor/... -run TestPlanAutoSetup_Resolution_Update_ProducesSetItem
check_go_test "[leg] keep resolution drops the conflict, keeps other items" \
  "$GO" -C backend test ./internal/radius/vendor/... -run TestPlanAutoSetup_Resolution_Keep_DropsConflictKeepsOtherItems
check_go_test "[leg] unresolved/abort/unknown are byte-identical to pre-FR-66" \
  "$GO" -C backend test ./internal/radius/vendor/... -run TestPlanAutoSetup_Resolution_UnresolvedOrAbort_MatchesPreFR66
check_go_test "[leg] non-resolvable conflict stays blocking even if 'update' is requested" \
  "$GO" -C backend test ./internal/radius/vendor/... -run TestPlanAutoSetup_Resolution_NotResolvable_UpdateFallsThroughToConflict
check_go_test "[leg] planHash differs across Values/Resolutions/NAS id" \
  "$GO" -C backend test ./internal/radius/... -run TestPlanHash_

# --- FR-67 server management (fake-router unit legs, item 8) ---------------
echo "-- FR-67 server management --"
check_go_test "[leg] hotspot create plan includes the FR-62.7 guard on its OWN profile" \
  "$GO" -C backend test ./internal/radius/vendor/... -run TestPlanService_CreateHotspot_IncludesFR627Guard
check_go_test "[leg] service-provisioning conflicts are abort-only (never Resolvable)" \
  "$GO" -C backend test ./internal/radius/vendor/... -run TestPlanService_Create_NameCollision_Conflicts
check_go_test "[leg] editing a missing router-side object conflicts, never silently creates" \
  "$GO" -C backend test ./internal/radius/vendor/... -run TestPlanService_Edit_RequiresExistingMatch

# --- DB-gated HTTP legs (items 3, 9, 10) ------------------------------------
echo "-- DB-gated HTTP legs --"
check_go_test "[leg] GET /nas/{id}/config: 422 no creds, happy-path snapshot" \
  "$GO" -C backend test ./internal/radius/... -run "TestNASConfig_"
check_go_test "[leg] service apply persists management_mode=system" \
  "$GO" -C backend test ./internal/radius/... -run TestServiceApply_CreateHotspot_PersistsSystemManaged
check_go_test "[leg] editing a router-managed service 409s before any router I/O" \
  "$GO" -C backend test ./internal/radius/... -run TestServiceEdit_RouterManaged_RequiresAdopt
check_go_test "[leg] adopt flips management_mode with ZERO router writes" \
  "$GO" -C backend test ./internal/radius/... -run TestServiceAdopt_FlipsModeWithZeroRouterWrites
check_go_test "[leg] adopt requires an explicit confirm" \
  "$GO" -C backend test ./internal/radius/... -run TestServiceAdopt_RequiresConfirm
check_go_test "[leg] adopting an already-system service 409s" \
  "$GO" -C backend test ./internal/radius/... -run TestServiceAdopt_AlreadySystem_Returns409

# --- Panel (item 11) ---------------------------------------------------------
echo "-- Panel --"
check "panel references the FR-65/67 API client functions" \
  grep -q "nasConfig\|planService\|adoptService" frontend/panel/src/api/nas.ts
check "panel build"  sh -c 'cd frontend/panel && npm run build'
check "panel lint"   sh -c 'cd frontend/panel && npm run lint'
check "panel vitest" sh -c 'cd frontend/panel && npx vitest run'
check "i18n:check (0 missing keys / 0 hardcoded strings)" sh -c 'cd frontend && node shared/scripts/i18n-check.mjs'

# --- Docs accuracy (item 12) -------------------------------------------------
echo "-- Docs --"
check "PRD carries FR-65..FR-67" \
  sh -c 'grep -q "FR-65" docs/PRD.md && grep -q "FR-66" docs/PRD.md && grep -q "FR-67" docs/PRD.md'
check "index coverage audit = 67/67" grep -q "67/67 owned" docs/prd/00-index.md
check "phase brief present" test -f docs/v2/phases/phase-v2-3-autosetup-config-manager/00-phase.md

echo
if [ "$FAILED" -eq 0 ]; then
  echo "== v2 phase 3 gate: ALL SCRIPTED LEGS PASSED =="
else
  echo "== v2 phase 3 gate: FAILURES ABOVE =="
fi
exit "$FAILED"
