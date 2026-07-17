#!/bin/sh
# gate-v2-phase-9.sh — machine-checkable legs of the v2 phase-9 integration
# gate (docs/v2/phases/phase-v2-9-cost-margin-pricing/00-phase.md). Each
# check prints a label followed by PASS or FAIL; exit status is non-zero if
# any leg fails. No human/hardware legs — this phase has no router/device
# dependency (per the phase brief's own "Human/hardware legs: none").
#
# Usage:  sh scripts/gate-v2-phase-9.sh
#   Set HIKRAD_TEST_DB_URL (+ HIKRAD_TEST_REDIS_URL) to exercise the DB-gated
#   legs (cost stamping, margin reconciliation, per-site isolation, reseller
#   pricing resolution, scoping). Without them the DB-gated Go legs self-skip
#   — and this script reports them FAIL, on purpose: skipped is not passed
#   for a gate whose point is proving the behavior.
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

echo "== v2 phase 9 gate (cost, margin & reseller pricing) =="

# --- Schema & migration (item 9) --------------------------------------------
echo "-- Schema & migration --"
for f in 0540_profile_cost_history 0541_overheads 0542_reseller_prices 0543_ledger_cost_at_sale; do
  check "migration ${f} present" test -f "backend/migrations/${f}.up.sql"
done
check "no .down.sql added (FR-51.4 forward-only)" \
  sh -c '! ls backend/migrations/054*.up.sql backend/migrations/054*.down.sql 2>/dev/null | grep -q "\.down\.sql$"'
check "backend builds" "$GO" -C backend build ./...
check "backend vets" "$GO" -C backend vet ./...
check "vendor isolation grep green (FR-17, unaffected by this phase)" sh scripts/lint-vendor-isolation.sh

# --- Item 8: no sub-reseller hierarchy exists in code -----------------------
echo "-- No sub-reseller tree (resolved kickoff blocker) --"
check "no v2-9 migration adds an ancestry/parent column to managers" \
  sh -c '! grep -rniE "ALTER TABLE managers|parent_manager|parent_reseller|manager.*ancestor|manager_hierarchy" backend/migrations/054*.sql'
check "profile_cost_history/overheads/reseller_prices carry no self-referential FK" \
  sh -c '! grep -nE "REFERENCES (profile_cost_history|overheads|reseller_prices)\(" backend/migrations/054*.sql'

# --- Item 1: cost resolution + unknown-cost safety (DB-gated, AC-71a) ------
echo "-- Cost resolution (DB-gated) --"
check_go_test "[leg] unknown cost stamps nil, never zero" \
  "$GO" -C backend test ./internal/billing/... -run TestUnknownCostStampsNilNeverZero
check_go_test "[leg] known cost stamped correctly, immune to later re-pricing" \
  "$GO" -C backend test ./internal/billing/... -run TestKnownCostStampsCorrectly

# --- Item 2: margin reconciliation (DB-gated, AC-72a) -----------------------
echo "-- Margin reconciliation (DB-gated) --"
check_go_test "[leg] sum(margin) = sum(revenue) - sum(cost) over known-cost rows; unknown still counts as revenue" \
  "$GO" -C backend test ./internal/reports/... -run TestMarginReconciliation

# --- Item 3: per-site overhead isolation (DB-gated, AC-73a) -----------------
echo "-- Per-site overhead isolation (DB-gated) --"
check_go_test "[leg] a site's net margin nets only its own overheads, never a share of global" \
  "$GO" -C backend test ./internal/reports/... -run TestSiteMarginNeverBlendsGlobal

# --- Items 4-5: reseller wholesale resolution (DB-gated, AC-74a/AC-74b) ----
echo "-- Reseller wholesale resolution (DB-gated) --"
check_go_test "[leg] no reseller_prices row = byte-identical to pre-v2-9 (retail debit)" \
  "$GO" -C backend test ./internal/billing/... -run TestRetailUnaffectedByNoResellerPrice
check_go_test "[leg] per-subscriber override beats plan-wide; subscribers always charged retail" \
  "$GO" -C backend test ./internal/billing/... -run TestPerSubscriberOverrideBeatsPlanWide

# --- Item 6: reseller-facing scoping never leaks (DB-gated, AC-75a) --------
echo "-- Reseller scoping (DB-gated) --"
check_go_test "[leg] a reseller-scoped margin response never contains cost/owner_margin/unknown_cost_count" \
  "$GO" -C backend test ./internal/reports/... -run TestResellerScopingNeverLeaksCost

# --- Item 7: independent leg resolution (DB-gated, AC-76a) ------------------
echo "-- Independent leg resolution (DB-gated) --"
check_go_test "[leg] subscriber's price_override unaffected by their reseller's wholesale price" \
  "$GO" -C backend test ./internal/billing/... -run TestPriceOverrideIndependentOfWholesale

# --- Item 9: full regression (DB-gated) -------------------------------------
echo "-- Full billing+reports regression (DB-gated) --"
check_go_test "[leg] full pre-existing internal/billing DB-gated suite (byte-identical when no v2-9 data exists)" \
  "$GO" -C backend test ./internal/billing/... -run "TestRenewWritesLedgerAndExtendsExpiry|TestBalanceBlockingAndTopup|TestBalanceEqualsLedgerProperty|TestVoucherDoubleRedeemRace|TestRefundReversalMath|TestLedgerImmutability|TestIdempotentRenew|TestPerCurrencyReconciliationInvariant|TestExchangePairCorrectness|TestNonIQDRenewalDebitsOnlyThatCurrency|TestRefundReversesOriginalCurrencyNoReResolution"
check_go_test "[leg] full pre-existing internal/reports DB-gated suite" \
  "$GO" -C backend test ./internal/reports/... -run "TestRevenueReportReconcilesWithPayments|TestSettlementClosingEqualsLiveBalance|TestExpiringReportMatchesDigestQuery|TestSettlementAndRevenueScopedManagerIsolation|TestDigestComposition"

# --- Item 10: panel build + lint + vitest + i18n:check ----------------------
echo "-- Panel/portal --"
check "panel references the v2-9 API client functions" \
  grep -q "listOverheads\|listResellerPrices\|getMarginReport" frontend/panel/src/api/billing.ts frontend/panel/src/api/reports.ts
check "panel pricing-admin + margin-report screens present" \
  sh -c 'test -f frontend/panel/src/pages/billing/PricingAdminPage.tsx && test -f frontend/panel/src/pages/reports/MarginReportPage.tsx'
check "shared build"  sh -c 'cd frontend && npm run build --workspace=@hikrad/shared'
check "panel build"   sh -c 'cd frontend/panel && npm run build'
check "panel lint"    sh -c 'cd frontend/panel && npm run lint'
check "panel vitest"  sh -c 'cd frontend/panel && npx vitest run'
check "i18n:check (0 missing keys / 0 hardcoded strings)" sh -c 'cd frontend && node shared/scripts/i18n-check.mjs'

# --- Item 11: docs accuracy --------------------------------------------------
echo "-- Docs --"
check "PRD carries FR-71..FR-76" \
  sh -c 'grep -q "FR-71" docs/PRD.md && grep -q "FR-74" docs/PRD.md && grep -q "FR-76" docs/PRD.md'
check "phase brief present" test -f docs/v2/phases/phase-v2-9-cost-margin-pricing/00-phase.md
check "known-issues.md carries this phase's date" grep -q "2026-07-17" docs/ops/known-issues.md

echo
if [ "$FAILED" -eq 0 ]; then
  echo "== v2 phase 9 gate: ALL SCRIPTED LEGS PASSED =="
else
  echo "== v2 phase 9 gate: FAILURES ABOVE =="
fi
exit "$FAILED"
