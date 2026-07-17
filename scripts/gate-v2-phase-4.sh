#!/bin/sh
# gate-v2-phase-4.sh — machine-checkable legs of the v2 phase-4 integration
# gate (docs/v2/phases/phase-v2-4-multi-currency/00-phase.md). Each check
# prints a label followed by PASS or FAIL; exit status is non-zero if any leg
# fails. No human/hardware legs — this phase has no router/device dependency
# (per the phase brief's own "Human/hardware legs: none").
#
# Usage:  sh scripts/gate-v2-phase-4.sh
#   Set HIKRAD_TEST_DB_URL (+ HIKRAD_TEST_REDIS_URL) to exercise the DB-gated
#   legs (migration backfill, reconciliation, exchange, non-IQD renewal,
#   refund). Without them the DB-gated Go legs self-skip — and this script
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

echo "== v2 phase 4 gate (multi-currency billing) =="

# --- Schema & migration (item 1, item 8) ------------------------------------
echo "-- Schema & migration --"
for f in 0530_currencies 0531_currency_rates 0532_ledger_currency \
         0533_manager_balances_currency 0534_manager_thresholds_currency \
         0535_profiles_currency 0536_voucher_batches_currency \
         0537_payments_currency 0538_payment_intents_currency; do
  check "migration ${f} present" test -f "backend/migrations/${f}.up.sql"
done
check "no .down.sql added (FR-51.4 forward-only)" \
  sh -c '! ls backend/migrations/053*.up.sql backend/migrations/053*.down.sql 2>/dev/null | grep -q "\.down\.sql$"'
check "backend builds" "$GO" -C backend build ./...
check "backend vets" "$GO" -C backend vet ./...
check "vendor isolation grep green (FR-17, unaffected by this phase)" sh scripts/lint-vendor-isolation.sh

# --- Gate item 1: migration backfill (DB-gated) -----------------------------
echo "-- Migration backfill (DB-gated) --"
check_go_test "[leg] currency migrations are lossless against v1-shaped data" \
  "$GO" -C backend test ./internal/billing/... -run TestCurrencyMigrationLossless

# --- Gate item 2: per-currency reconciliation invariant (DB-gated, AC-69c) --
echo "-- Per-currency reconciliation (DB-gated) --"
check_go_test "[leg] balance(M,C) = sum(ledger where M,C), independently per currency" \
  "$GO" -C backend test ./internal/billing/... -run TestPerCurrencyReconciliationInvariant

# --- Gate item 3: exchange pair (DB-gated, AC-69b) --------------------------
echo "-- Exchange (DB-gated) --"
check_go_test "[leg] exchange writes exactly 2 linked rows, correct signs/amounts, no leak to other currencies" \
  "$GO" -C backend test ./internal/billing/... -run TestExchangePairCorrectness

# --- Gate item 4: non-IQD renewal (DB-gated, AC-69a) ------------------------
echo "-- Non-IQD renewal (DB-gated) --"
check_go_test "[leg] a USD renewal debits only the USD balance; IQD balance untouched" \
  "$GO" -C backend test ./internal/billing/... -run TestNonIQDRenewalDebitsOnlyThatCurrency

# --- Gate item 5: refund reverses original currency (DB-gated, AC-69d) -----
echo "-- Refund original-currency (DB-gated) --"
check_go_test "[leg] refund reverses in the original currency, never re-resolved" \
  "$GO" -C backend test ./internal/billing/... -run TestRefundReversesOriginalCurrencyNoReResolution

# --- Gate item 6: no online rate feed (AC-68b) ------------------------------
echo "-- No online rate feed (AC-68b) --"
check "no HTTP client construction in the currency-rates/exchange file" \
  sh -c '! grep -nE "http\.(Client|Get|Post|NewRequest)" backend/internal/billing/balance_api.go'

# --- Gate item 7: formatMoney regression lock (AC-70a) ----------------------
echo "-- formatMoney regression lock (AC-70a) --"
check "shared package test suite (includes the AC-70a regression lock)" \
  sh -c 'cd frontend && npx vitest run --root shared'

# --- Gate item 8: migration lossless + build + full billing suite ----------
echo "-- Full billing suite (DB-gated) --"
check_go_test "[leg] full internal/billing DB-gated suite (every *_iqd assertion renamed, same values)" \
  "$GO" -C backend test ./internal/billing/... -run "TestRenewWritesLedgerAndExtendsExpiry|TestBalanceBlockingAndTopup|TestBalanceEqualsLedgerProperty|TestVoucherDoubleRedeemRace|TestRefundReversalMath|TestLedgerImmutability|TestIdempotentRenew"

# --- Gate item 9: panel/portal build + lint + vitest + i18n:check ----------
echo "-- Panel/portal --"
check "panel references the currency API client functions" \
  grep -q "listCurrencies\|exchangeManagerBalance\|listCurrencyRates" frontend/panel/src/api/billing.ts
check "panel currency-rates + exchange screen present" \
  test -f frontend/panel/src/pages/billing/CurrencyRatesPage.tsx
check "shared build"  sh -c 'cd frontend && npm run build --workspace=@hikrad/shared'
check "panel build"   sh -c 'cd frontend/panel && npm run build'
check "portal build"  sh -c 'cd frontend/portal && npm run build'
check "panel lint"    sh -c 'cd frontend/panel && npm run lint'
check "portal lint"   sh -c 'cd frontend/portal && npm run lint'
check "panel vitest"  sh -c 'cd frontend/panel && npx vitest run'
check "portal vitest" sh -c 'cd frontend/portal && npx vitest run'
check "i18n:check (0 missing keys / 0 hardcoded strings)" sh -c 'cd frontend && node shared/scripts/i18n-check.mjs'

# --- Gate item 10: docs accuracy --------------------------------------------
echo "-- Docs --"
check "PRD carries FR-68..FR-70" \
  sh -c 'grep -q "FR-68" docs/PRD.md && grep -q "FR-69" docs/PRD.md && grep -q "FR-70" docs/PRD.md'
check "phase brief present" test -f docs/v2/phases/phase-v2-4-multi-currency/00-phase.md
check "known-issues.md carries this phase's date" grep -q "2026-07-17" docs/ops/known-issues.md

echo
if [ "$FAILED" -eq 0 ]; then
  echo "== v2 phase 4 gate: ALL SCRIPTED LEGS PASSED =="
else
  echo "== v2 phase 4 gate: FAILURES ABOVE =="
fi
exit "$FAILED"
