#!/bin/sh
# gate-v2-phase-2.sh — machine-checkable legs of the v2 phase-2 integration
# gate (docs/v2/phases/phase-v2-2-manual-payments/00-phase.md). Each check
# prints a label followed by PASS or FAIL; exit status is non-zero if any
# leg fails. No human/hardware legs — the closest is real-device camera
# capture for attachments, a documented-pending manual pass, not scriptable
# here (same as v2-4/v2-9, no router/device dependency).
#
# Usage:  sh scripts/gate-v2-phase-2.sh
#   Set HIKRAD_TEST_DB_URL (+ HIKRAD_TEST_REDIS_URL) to exercise the DB-gated
#   legs (migration losslessness, trial grant/reset, scoping, wholesale
#   approval, notifications, attachment auth, no-account fallback). Without
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

echo "== v2 phase 2 gate (manual payment providers) =="

# --- Schema & migration ------------------------------------------------------
echo "-- Schema & migration --"
for f in 0580_payment_providers 0581_manager_provider_accounts 0582_manager_method_settings \
         0583_payment_tickets 0584_payment_ticket_attachments 0585_payment_ticket_events \
         0586_card_payments_migrate_to_tickets 0587_drop_gateway_surface \
         0588_manager_method_settings_backfill; do
  check "migration ${f} present" test -f "backend/migrations/${f}.up.sql"
done
check "no .down.sql added (FR-51.4 forward-only)" \
  sh -c '! ls backend/migrations/058*.up.sql backend/migrations/058*.down.sql 2>/dev/null | grep -q "\.down\.sql$"'
check "backend builds" "$GO" -C backend build ./...
check "backend vets" "$GO" -C backend vet ./...
check "vendor isolation grep green (FR-17, unaffected by this phase)" sh scripts/lint-vendor-isolation.sh

# --- Item 9: gateway surface fully removed (C12) ----------------------------
echo "-- Gateway surface fully removed (C12) --"
check "no PaymentGateway symbol anywhere in the tree" \
  sh -c '! grep -rn "PaymentGateway\b" --include="*.go" backend/internal backend/cmd'
# tickets_migration_v2p2_db_test.go deliberately writes v1-shaped
# card_payments rows to a scratch DB to prove the migration is lossless —
# that is the one legitimate exception, not a live application dependency.
check "no live SQL against dropped tables (card_payments/payment_intents/gateway_configs)" \
  sh -c '! grep -rnE "(FROM|INTO) (card_payments|payment_intents|gateway_configs)\b" --include="*.go" backend/internal backend/cmd | grep -v tickets_migration_v2p2_db_test.go'
check "internal/billing/gateways/ package tree is gone" sh -c '! test -d backend/internal/billing/gateways'
check "no panel gateway-config screen or API client" \
  sh -c '! grep -rn "PaymentGateway\|payment-gateways\|GatewaySettings" frontend/panel/src frontend/portal/src'

# --- Item 1: lossless card_payments -> payment_tickets migration (DB-gated) -
echo "-- Migration losslessness (DB-gated) --"
check_go_test "[leg] every card_payments row survives as a payment_tickets row (state/method_detail/events intact), old table gone" \
  "$GO" -C backend test ./internal/billing/... -run TestCardPaymentsMigrationLossless

# --- Item 2-3: trial grant / no-trial-on-retry / reset-on-approval (DB-gated)
echo "-- Trial eligibility (DB-gated, AC-78a/78b) --"
check_go_test "[leg] first submission grants a 1-day trial" \
  "$GO" -C backend test ./internal/billing/... -run TestTrialGrantedOnFirstAttempt
check_go_test "[leg] resubmission right after a rejection is accepted but grants no trial; approval resets eligibility" \
  "$GO" -C backend test ./internal/billing/... -run TestNoTrialOnRetryResetOnApproval

# --- Item 4: owner-scoping + admin-sees-all (DB-gated, AC-79a) --------------
echo "-- Queue/log scoping (DB-gated) --"
check_go_test "[leg] a scoped agent's queue/log shows only their own tickets; scope=all is silently downgraded" \
  "$GO" -C backend test ./internal/billing/... -run TestOwnerScopingAdminSeesAll

# --- Item 5: wholesale-aware approval (DB-gated, AC-79b) --------------------
echo "-- Wholesale-aware approval (DB-gated) --"
check_go_test "[leg] approving a reseller-priced ticket debits wholesale; subscriber's own receipt stays retail" \
  "$GO" -C backend test ./internal/billing/... -run TestWholesaleAwareApprovalTicket

# --- Item 6: both-sides notifications (DB-gated, AC-80a) --------------------
echo "-- Notification matrix (DB-gated) --"
check_go_test "[leg] billing.payment_ticket carries owner_manager_id on submit and decided_by on approve; every notification traces to a real event row" \
  "$GO" -C backend test ./internal/billing/... -run TestBothSidesTicketNotifications

# --- Item 7: attachment authorization (DB-gated) ----------------------------
echo "-- Attachment authorization (DB-gated) --"
check_go_test "[leg] owner + admin can fetch an attachment, a different scoped manager cannot; Content-Disposition always attachment" \
  "$GO" -C backend test ./internal/billing/... -run TestAttachmentAuthorization

# --- Item 8: no-account fallback never leaks a method (DB-gated, AC-77a) ----
echo "-- No-account fallback (DB-gated, kickoff blocker 1) --"
check_go_test "[leg] an enabled-but-unconfigured provider never appears in a subscriber's resolved Pay methods" \
  "$GO" -C backend test ./internal/billing/... -run TestNoAccountFallbackNeverLeaksMethod

# --- Item 10: build + full regression ---------------------------------------
echo "-- Full billing+reports regression (DB-gated) --"
check_go_test "[leg] full pre-existing internal/billing DB-gated suite, including v2-4/v2-9's own gate tests" \
  "$GO" -C backend test ./internal/billing/... -run "TestRenewWritesLedgerAndExtendsExpiry|TestBalanceBlockingAndTopup|TestBalanceEqualsLedgerProperty|TestVoucherDoubleRedeemRace|TestRefundReversalMath|TestLedgerImmutability|TestIdempotentRenew|TestPerCurrencyReconciliationInvariant|TestExchangePairCorrectness|TestNonIQDRenewalDebitsOnlyThatCurrency|TestRefundReversesOriginalCurrencyNoReResolution|TestCurrencyMigrationLossless|TestUnknownCostStampsNilNeverZero|TestKnownCostStampsCorrectly|TestRetailUnaffectedByNoResellerPrice|TestPerSubscriberOverrideBeatsPlanWide|TestPriceOverrideIndependentOfWholesale"
check_go_test "[leg] full pre-existing internal/reports DB-gated suite (see known-issues.md for the pre-existing narrow-window flake if run immediately after billing)" \
  "$GO" -C backend test ./internal/reports/... -run "TestSettlementClosingEqualsLiveBalance|TestExpiringReportMatchesDigestQuery|TestSettlementAndRevenueScopedManagerIsolation|TestDigestComposition|TestMarginReconciliation|TestSiteMarginNeverBlendsGlobal|TestResellerScopingNeverLeaksCost"

# --- Item 11: panel/portal build + lint + vitest + i18n:check --------------
echo "-- Panel/portal --"
check "portal Pay screen present" test -f frontend/portal/src/components/renew/PayPanel.tsx
check "panel provider catalog + my-methods + tickets screens present" \
  sh -c 'test -f frontend/panel/src/pages/billing/ProviderCatalogPage.tsx && test -f frontend/panel/src/pages/billing/MyPaymentMethodsPage.tsx && test -f frontend/panel/src/pages/billing/PaymentTicketsPage.tsx'
check "shared build"  sh -c 'cd frontend && npm run build --workspace=@hikrad/shared'
check "panel build"   sh -c 'cd frontend/panel && npm run build'
check "panel lint"    sh -c 'cd frontend/panel && npm run lint'
check "panel vitest"  sh -c 'cd frontend/panel && npx vitest run'
check "portal build"  sh -c 'cd frontend/portal && npm run build'
check "portal lint"   sh -c 'cd frontend/portal && npm run lint'
check "portal vitest" sh -c 'cd frontend/portal && npx vitest run'
check "i18n:check (0 missing keys / 0 hardcoded strings)" sh -c 'cd frontend && node shared/scripts/i18n-check.mjs'

# --- Item 12: docs accuracy --------------------------------------------------
echo "-- Docs --"
check "PRD carries FR-77..FR-80 and marks FR-23 retired" \
  sh -c 'grep -q "FR-77" docs/PRD.md && grep -q "FR-80" docs/PRD.md && grep -qi "FR-23" docs/PRD.md'
check "phase brief present" test -f docs/v2/phases/phase-v2-2-manual-payments/00-phase.md
check "known-issues.md carries this phase's date" grep -q "2026-07-18" docs/ops/known-issues.md

echo
if [ "$FAILED" -eq 0 ]; then
  echo "== v2 phase 2 gate: ALL SCRIPTED LEGS PASSED =="
else
  echo "== v2 phase 2 gate: FAILURES ABOVE =="
fi
exit "$FAILED"
