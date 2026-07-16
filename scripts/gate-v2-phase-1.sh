#!/bin/sh
# gate-v2-phase-1.sh — machine-checkable legs of the v2 phase-1 integration
# gate (docs/v2/phases/phase-v2-1-hotspot-management/00-phase.md). Each check
# prints a label followed by PASS or FAIL; exit status is non-zero if any leg
# fails. Human/hardware legs (live multi-service auth on a real MikroTik;
# auto-setup apply of a multi-service snippet) are documented-pending in
# gate-result.md and NOT scripted here.
#
# Usage:  sh scripts/gate-v2-phase-1.sh
#   Set HIKRAD_TEST_DB_URL (+ HIKRAD_TEST_REDIS_URL) to exercise the DB-backed
#   legs (migration, service-matrix, scoping). Without them the DB-gated Go
#   legs self-skip — and this script reports them FAIL, on purpose: skipped is
#   not passed for a gate whose point is proving the behavior.
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
# check_go_test: `go test` exits 0 both when a test passes AND when it self-skips
# (t.Skip on a missing HIKRAD_TEST_DB_URL); require PASS with no SKIP so a
# DB-gated leg cannot report PASS without actually running.
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

echo "== v2 phase 1 gate (hotspot management + NAS scoping) =="

# --- Schema & migrations (0500-0519 range; gate item 1) --------------------
echo "-- Schema & migrations --"
check "migration 0500_subscriber_service_type present" test -f backend/migrations/0500_subscriber_service_type.up.sql
check "migration 0501_nas_services present"             test -f backend/migrations/0501_nas_services.up.sql
check "migration 0502_nas_scoping present"              test -f backend/migrations/0502_nas_scoping.up.sql
check "0500 backfills allow_hotspot losslessly"         grep -Eq "allow_hotspot" backend/migrations/0500_subscriber_service_type.up.sql
check "0500 retires allow_hotspot"                      grep -Eiq "DROP COLUMN( IF EXISTS)? allow_hotspot" backend/migrations/0500_subscriber_service_type.up.sql
check "0501 backfills from nas.type + retires it"       grep -Eiq "DROP COLUMN( IF EXISTS)? type" backend/migrations/0501_nas_services.up.sql
check "no migration outside the 0500-0519 range added"  sh -c '! ls backend/migrations/ | grep -Eq "^05[2-9][0-9]_"'
# Forward-only per FR-51.4 (docs/ops/update.md: "there is no down-migration path
# in production"), which supersedes this phase doc's original paired-.down.sql
# requirement — see the migration-range note in 00-phase.md. A .down.sql here
# would be the only one in the repo AND would be lossy: service_type has three
# values and allow_hotspot two, so hotspot+dual collapse and a down-then-up
# round trip would silently grant hotspot-only accounts PPPoE.
check "no .down.sql added (FR-51.4 forward-only)"       sh -c '! ls backend/migrations/ | grep -q "\.down\.sql$"'

# --- Backend model (C2/C3/C4) ----------------------------------------------
echo "-- Backend model --"
check "AuthView carries ServiceType (replaces AllowHotspot)" grep -qE '^\s+ServiceType\s+string' backend/internal/radius/authview.go
# Match a FIELD DECLARATION, not any mention: the struct's doc comment explains
# what AllowHotspot used to mean (dual == the old true), which is exactly the
# context a future reader needs, and a bare grep would forbid saying so.
check "AllowHotspot removed from AuthView"                   sh -c '! grep -qE "^\s+AllowHotspot\s+bool" backend/internal/radius/authview.go'
# FR-64 scope. Amended 2026-07-16 with C4: the scope is a SET (Scopes []NASScope),
# not the single AssignedNASID/AssignedServiceID pair this leg originally checked.
# Assert the field AND that the retired pair is gone, so the two shapes cannot
# coexist and drift.
check "AuthView carries the FR-64 scope set"                grep -qE '^\s+Scopes\s+\[\]NASScope' backend/internal/radius/authview.go
check "AuthView's single-pair scope fields removed"         sh -c '! grep -qE "^\s+Assigned(NAS|Service)ID\s+string" backend/internal/radius/authview.go'
check "an empty scope set means ANY NAS, not none"          grep -q "func TestScopeAllowsEmptySetMeansAnyNAS" backend/internal/radius/service_type_test.go
check "nas_not_allowed reject reason added"                 grep -q 'ReasonNASNotAllowed = "nas_not_allowed"' backend/internal/radius/intents.go
check "loader selects service_type"                        grep -q "service_type" backend/internal/subscribers/authview.go
check "bulk action renamed set_service_type"               grep -q "set_service_type" backend/internal/subscribers/bulk.go
check "no lingering set_allow_hotspot bulk action"         sh -c '! grep -q "set_allow_hotspot" backend/internal/subscribers/bulk.go'

# --- RADIUS engine + vendor seam (C6/C7) -----------------------------------
echo "-- RADIUS engine --"
check "authorizeRequest forwards Called-Station-Id"        grep -q "CalledStationID" backend/internal/radius/authorize.go
check "vendor adapter exposes ResolveService (FR-17 seam)" grep -q "ResolveService" backend/internal/radius/vendor/vendor.go
check "FreeRADIUS bridge forwards called_station_id"       grep -q "called_station_id" deploy/freeradius/scripts/authorize.pl
check "vendor isolation grep green (FR-17)"                sh scripts/lint-vendor-isolation.sh
check "backend builds"                                     "$GO" -C backend build ./...
check "backend vet"                                        "$GO" -C backend vet ./...

# --- DB-gated behavior legs (gate items 1,2,3,5,6) -------------------------
echo "-- DB-gated behavior legs --"
check_go_test "[leg] lossless allow_hotspot->service_type + nas.type->nas_services migration (item 1)" \
  "$GO" -C backend test ./internal/subscribers/... -run TestServiceTypeMigrationLossless
check_go_test "[leg] Phase-2 pppoe/dual policy regression unchanged (item 2)" \
  "$GO" -C backend test ./internal/radius/... -run "TestAuthorize"
check_go_test "[leg] service-type matrix: hotspot-only + dual + pppoe (item 3)" \
  "$GO" -C backend test ./internal/radius/... -run TestServiceTypeMatrix
check_go_test "[leg] multi-service NAS instance resolution + per-instance pool (item 4)" \
  "$GO" -C backend test ./internal/radius/... -run TestMultiServiceNAS
check_go_test "[leg] NAS scoping nas_not_allowed + subscriber-over-profile (item 5)" \
  "$GO" -C backend test ./internal/radius/... -run TestNASScoping
check_go_test "[leg] no-pool-anywhere omits address_pool intent (item 6)" \
  "$GO" -C backend test ./internal/radius/... -run TestNoPoolOmitsAddressPool

# --- Panel (gate item 8) ---------------------------------------------------
echo "-- Panel --"
check "subscriber form has a service-type selector" grep -rqi "service_type\|serviceType" frontend/panel/src/pages/subscribers
check "E TypeScript build"          sh -c 'cd frontend/panel && npm run build'
check "E lint"                      sh -c 'cd frontend/panel && npm run lint'
check "E vitest"                    sh -c 'cd frontend/panel && npx vitest run'
check "i18n:check (0 missing keys / 0 hardcoded strings)" sh -c 'cd frontend && node shared/scripts/i18n-check.mjs'

# --- Docs accuracy (gate item 9) -------------------------------------------
echo "-- Docs --"
check "PRD carries FR-61..FR-64"    sh -c 'grep -q "FR-61" docs/PRD.md && grep -q "FR-62" docs/PRD.md && grep -q "FR-63" docs/PRD.md && grep -q "FR-64" docs/PRD.md'
check "index coverage audit = 64/64" grep -q "64/64 owned" docs/prd/00-index.md
check "phase brief present"          test -f docs/v2/phases/phase-v2-1-hotspot-management/00-phase.md

echo
if [ "$FAILED" -eq 0 ]; then
  echo "== v2 phase 1 gate: ALL SCRIPTED LEGS PASSED =="
else
  echo "== v2 phase 1 gate: FAILURES ABOVE =="
fi
exit "$FAILED"
