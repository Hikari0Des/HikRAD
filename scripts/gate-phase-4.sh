#!/bin/sh
# gate-phase-4.sh — machine-checkable legs of the Phase-4 integration gate
# (docs/phases/phase-4-portal-payments-pwa/00-phase.md). Each agent appends its
# own legs below; every check prints a label followed by PASS or FAIL. Exit
# status is non-zero if any leg fails.
#
# Physical/UX legs (gate items 1, 3, 4, 7) are human-run and are NOT scripted
# here. Scriptable legs per the phase amendment: items 2, 6, 9 (fake path),
# 10.
#
# Usage:  sh scripts/gate-phase-4.sh
#   Set HIKRAD_TEST_DB_URL (+ HIKRAD_TEST_REDIS_URL) to exercise the DB-backed
#   legs; without them the Go suites self-skip their integration tests.
set -u

# Resolve repo root (this script lives in scripts/).
ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$ROOT" || exit 2

FAILED=0
pass() { printf '  [PASS] %s\n' "$1"; }
fail() { printf '  [FAIL] %s\n' "$1"; FAILED=1; }
check() { # check "<label>" <cmd...>
  label=$1; shift
  if "$@" >/dev/null 2>&1; then pass "$label"; else fail "$label"; fi
}

# Locate the Go toolchain (CI has it on PATH; be lenient locally).
GO=go
command -v go >/dev/null 2>&1 || {
  for c in "/c/Program Files/Go/bin/go" "/usr/local/go/bin/go"; do
    [ -x "$c" ] && GO=$c && break
  done
}

echo "== Phase 4 gate =="

# ---------------------------------------------------------------------------
# Agent 2 — Accounting & Monitoring (gate item 5: panel push; item 9: WhatsApp
# subscriber messaging incl. request-capture fake fallback)
# ---------------------------------------------------------------------------
echo "-- Agent 2: Accounting & Monitoring --"

# C1-C schema present.
check "C2 migration 0330_push_subscriptions present" test -f backend/migrations/0330_push_subscriptions.up.sql
check "C2 push_subscriptions one-owner constraint" grep -q "surface = 'panel'" backend/migrations/0330_push_subscriptions.up.sql

# C4 Web Push backend: VAPID bootstrap, subscribe route, panel alert channel.
check "C2 push module exists" test -f backend/internal/push/module.go
check "C2 push subscribe route" grep -rq "/api/v1/push/subscribe" backend/internal/push
check "C2 VAPID bootstrap (EnsureKeys)" grep -rq "func EnsureKeys" backend/internal/push
check "C2 push channel wired into alert engine" grep -q "pushSender{}" backend/internal/monitorsvc/alerts.go
check "C2 push channel in frozen enum" grep -q "chPush: true" backend/internal/monitorsvc/rules_api.go
check "C2 push module mounted" grep -q "internal/push" backend/cmd/hikrad-api/modules.go
# Payload shape is keys+params only, never rendered text (4 KB cap edge case).
check "C2 payload cap (never rendered text)" grep -q "maxPayloadBytes" backend/internal/push/webpush.go

# C7/C8 subscriber WhatsApp messaging (FR-55/FR-59): consumes D's events,
# per-subscriber expiry targeting, delivery isolation, request-capture fake
# override for when Meta onboarding is pending (gate item 9's fallback).
check "C2 consumes billing.renewed" grep -rq "billing.renewed" backend/internal/monitorsvc/subscriber_events.go
check "C2 consumes billing.card_payment" grep -rq "billing.card_payment" backend/internal/monitorsvc/subscriber_events.go
check "C2 per-subscriber expiry targeting" grep -q "digestPerSubscriber" backend/internal/monitorsvc/conditions.go
check "C2 whatsapp graph URL overridable (request-capture fake)" grep -rq "var graphAPIBase" backend/internal/monitorsvc/subscriber_whatsapp.go

# Usage API polish (task 5): Asia/Baghdad-aware bucketing + response cap,
# exported for D's portal usage endpoint to call directly.
check "C2 usage bucketing is Baghdad-aware" grep -q "Asia/Baghdad" backend/internal/live/usage.go
check "C2 usage response-size cap" grep -q "maxUsagePoints" backend/internal/live/usage.go
check "C2 usage exported for D's portal endpoint" grep -q "func UsageForSubscriber" backend/internal/live/usage.go

# Accounting & Monitoring + push suites: VAPID idempotence, RFC 8291
# encryption round-trip, 410 pruning/dedup, channel/delivery isolation,
# Baghdad month-boundary math, empty-history/response-cap (DB-backed cases
# self-skip without HIKRAD_TEST_DB_URL).
( cd backend && "$GO" test ./internal/push/... >/dev/null 2>&1 ) \
  && pass "C2 push suite (go test ./internal/push/...)" \
  || fail "C2 push suite (go test ./internal/push/...)"
( cd backend && "$GO" test ./internal/monitorsvc/... ./internal/live/... >/dev/null 2>&1 ) \
  && pass "C2 monitorsvc+live suite" \
  || fail "C2 monitorsvc+live suite"

# ---------------------------------------------------------------------------
# Agent 1 — RADIUS & NAS (gate item 7: ROS matrix; gate item 8: NAS API
# auto-setup; gate item 1's expired->renew->CoA-restore leg via the walled
# garden). Physical ROS 6.49/7.x hardware/CHR legs are human-run (docs/ops/
# ros-matrix.md §5) and NOT scripted here; everything below is the code-level
# proof: quirk logic, additive-only/conflict-abort/hash-staleness semantics,
# vendor isolation, and CoA hardening.
# ---------------------------------------------------------------------------
echo "-- Agent 1: RADIUS & NAS --"

# Gate item 7: ROS quirk matrix doc + code-encoded findings + scripted suite.
check "B ros-matrix doc present" test -f docs/ops/ros-matrix.md
check "B ros-matrix doc covers both targets" grep -q "6.49" docs/ops/ros-matrix.md
check "B SupportsInPlace quirk gating (FR-15.4, version-aware)" grep -q "func (mikrotikAdapter) SupportsInPlace" backend/internal/radius/vendor/mikrotik_autosetup.go
check "B harness ros-matrix mode wired" grep -q '"ros-matrix"' backend/test/harness/main.go

# Gate item 8: NAS API auto-setup (C6) — migration, endpoints, per-ROS gate,
# vendor-only RouterOS API client (FR-56.4/FR-17.1).
check "B migration 0320 (nas api_port/api_user/api_password_enc)" test -f backend/migrations/0320_nas_api_setup.up.sql
check "B auto-setup preview route" grep -q "auto-setup/preview" backend/internal/radius/module.go
check "B auto-setup apply route" grep -q "auto-setup/apply" backend/internal/radius/module.go
check "B apply refuses on an unvalidated ROS version" grep -q "rosMatrixValidated" backend/internal/radius/autosetup_api.go
check "B RouterOS API client stays inside the vendor adapter" test -f backend/internal/radius/vendor/routeros_conn.go
( sh scripts/lint-vendor-isolation.sh >/dev/null 2>&1 ) \
  && pass "B vendor-isolation lint (no Mikrotik-* outside internal/radius/vendor/)" \
  || fail "B vendor-isolation lint (no Mikrotik-* outside internal/radius/vendor/)"

# CoA hardening (task 4): storm-safety caps + per-op/result metrics for C's
# health page.
check "B CoA storm-safety cap (coaMaxInflight)" grep -q "coaMaxInflight" backend/internal/radius/coa.go
check "B enforcement worker fan-out cap (enforceMaxConcurrent)" grep -q "enforceMaxConcurrent" backend/internal/radius/enforce.go
check "B CoA per-op/result metrics (coa:metrics)" grep -q "CoAMetricsKey" backend/internal/radius/coa.go

# Full suites: vendor-package quirk/auto-setup-plan unit tests (no DB/network
# needed) and the radius package (DB-backed auto-setup negative tests —
# planted conflict, hash-staleness, wrong credentials — self-skip without
# HIKRAD_TEST_DB_URL, PASS this repo's real-Postgres run performed during
# development).
( cd backend && "$GO" test ./internal/radius/vendor/... >/dev/null 2>&1 ) \
  && pass "B vendor suite (ROS quirk matrix + auto-setup planner)" \
  || fail "B vendor suite (ROS quirk matrix + auto-setup planner)"
( cd backend && "$GO" test ./internal/radius/... >/dev/null 2>&1 ) \
  && pass "B radius suite (incl. auto-setup negative tests)" \
  || fail "B radius suite (incl. auto-setup negative tests)"
# Scoped to B's own packages + the deployable binary, not ./... — a
# concurrent agent's in-progress package elsewhere in the monorepo shouldn't
# fail *this* leg (see 00-team.md's concurrent-agents note).
( cd backend && "$GO" build ./internal/radius/... ./cmd/hikrad-api/... ./test/harness/... >/dev/null 2>&1 ) \
  && pass "B packages + hikrad-api build cleanly" \
  || fail "B packages + hikrad-api build cleanly"
( cd backend && "$GO" test ./test/harness/... >/dev/null 2>&1 ) \
  && pass "B harness suite" \
  || fail "B harness suite"

# ---------------------------------------------------------------------------
# Agent 4 — Frontend Portal & Localization (gate items 1/3/4 client halves:
# portal UI + PWA packaging of both apps; item 5's client half: push opt-in).
# Physical legs (real-phone Arabic run-through, actual install/offline/update
# on a device, a push notification actually arriving) are human-run and NOT
# scripted here — this proves the code that backs them exists and is green.
# ---------------------------------------------------------------------------
echo "-- Agent 4: Frontend Portal & Localization --"

# Portal API/auth layer against contract C2 (subscriber-scoped, IDOR rule:
# identity from the token only, no subscriber_id params anywhere client-side).
for f in client tokenStore auth me usage payments vouchers cardPayments push branding; do
  case "$f" in
    tokenStore) check "F portal auth/tokenStore.ts present" test -f frontend/portal/src/auth/tokenStore.ts ;;
    auth) check "F portal api/auth.ts present" test -f frontend/portal/src/api/auth.ts ;;
    *) check "F portal api/${f}.ts present" test -f "frontend/portal/src/api/${f}.ts" ;;
  esac
done

# Hero flows present: home (FR-41.2, no quota ceiling — Decision 21), usage +
# payments (FR-41.3), renew incl. voucher/gateway/scratch-card (FR-42/FR-59),
# account self-care (FR-44), payment-return polling (deep-link safe).
check "F home screen (FR-41.2)"          test -f frontend/portal/src/pages/HomePage.tsx
check "F home never renders a QuotaBar (Decision 21)" sh -c '! grep -q "QuotaBar" frontend/portal/src/pages/HomePage.tsx'
check "F usage + payment history (FR-41.3)" test -f frontend/portal/src/pages/UsagePage.tsx
check "F renew: voucher/gateway/card tabs (FR-42/59)" test -f frontend/portal/src/pages/RenewPage.tsx
check "F voucher panel"                  test -f frontend/portal/src/components/renew/VoucherPanel.tsx
check "F gateway panel"                  test -f frontend/portal/src/components/renew/GatewayPanel.tsx
check "F scratch-card panel"             test -f frontend/portal/src/components/renew/ScratchCardPanel.tsx
check "F account self-care (FR-44)"      test -f frontend/portal/src/pages/SettingsPage.tsx
check "F payment return/poll route"      test -f frontend/portal/src/pages/PaymentReturnPage.tsx

# PWA packaging both apps (FR-54): manifest + SW + install education + update
# toast + offline banner, portal own scope and the panel cross-boundary
# exception (Agent E unstaffed this phase).
for app in portal panel; do
  check "F ${app} manifest.webmanifest"   test -f "frontend/${app}/public/manifest.webmanifest"
  check "F ${app} service worker"         test -f "frontend/${app}/public/sw.js"
  check "F ${app} offline fallback page"  test -f "frontend/${app}/public/offline.html"
  check "F ${app} SW registration"        test -f "frontend/${app}/src/pwa/registerServiceWorker.ts"
  check "F ${app} update toast"           test -f "frontend/${app}/src/pwa/UpdateToast.tsx"
  check "F ${app} offline banner"         test -f "frontend/${app}/src/pwa/OfflineBanner.tsx"
  check "F ${app} install prompt/education" test -f "frontend/${app}/src/pwa/InstallBanner.tsx"
  check "F ${app} branded manifest swap"  test -f "frontend/${app}/src/pwa/BrandedManifestLink.tsx"
done
check "F panel push opt-in (C4, item 5 client half)" test -f frontend/panel/src/pwa/PushOptIn.tsx
check "F panel notification click-through (task 6)"  test -f frontend/panel/src/pwa/NotificationClickRouter.tsx
check "F panel PWA exception documented"  test -f frontend/panel/src/pwa/README.md

# Machine-checkable UI gate: type-check + build, unit/component tests
# (renew flow states, intent polling, offline trigger, SW update, language
# persistence), no hardcoded user-visible strings (en/ar/ku key parity).
( cd frontend/portal && npm run build >/dev/null 2>&1 ) \
  && pass "F portal build (tsc -b && vite build)" \
  || fail "F portal build (tsc -b && vite build)"
( cd frontend/portal && npx vitest run >/dev/null 2>&1 ) \
  && pass "F portal tests (vitest run)" \
  || fail "F portal tests (vitest run)"
( cd frontend/portal && npm run lint >/dev/null 2>&1 ) \
  && pass "F portal lint (eslint + prettier + stylelint)" \
  || fail "F portal lint (eslint + prettier + stylelint)"
( cd frontend/panel && npm run build >/dev/null 2>&1 ) \
  && pass "F panel build still green after the PWA exception scope" \
  || fail "F panel build still green after the PWA exception scope"
( cd frontend/panel && npx vitest run >/dev/null 2>&1 ) \
  && pass "F panel tests still green after the PWA exception scope" \
  || fail "F panel tests still green after the PWA exception scope"
( cd frontend && npm run i18n:check >/dev/null 2>&1 ) \
  && pass "F i18n:check (no hardcoded strings, en/ar/ku parity)" \
  || fail "F i18n:check (no hardcoded strings, en/ar/ku parity)"

# ---------------------------------------------------------------------------
# Agent 3 — Backend Business (gate item 1 API half: portal login/me/renewal;
# item 2: mock e-wallet lifecycle incl. replay-proofing + reconciliation +
# graceful-degradation; item 6: IDOR + portal rate-limit; item 10: scratch-card
# payments incl. trial/approve/reject math and abuse guards).
# ---------------------------------------------------------------------------
echo "-- Agent 3: Backend Business --"

# C1-D schema present (0300-0304, exclusive range).
for f in 0300_portal_sessions 0301_subscriber_language 0302_payment_intents 0303_gateway_configs 0304_card_payments; do
  check "D3 migration $f present" test -f "backend/migrations/${f}.up.sql"
done

# Race-proof invariants at the DB level (never re-derivable from code alone):
# idempotent callback correlation (unique gateway+gateway_ref) and the C8
# abuse guard (at most one pending card payment per subscriber).
check "D3 payment_intents gateway_ref unique index" grep -q "payment_intents_gateway_ref_idx" backend/migrations/0302_payment_intents.up.sql
check "D3 card_payments one-pending-per-subscriber guard" grep -q "card_payments_one_pending_idx" backend/migrations/0304_card_payments.up.sql

# Portal API surface (C2 FR-41/42/43/44) — subscriber-scoped, IDOR rule:
# identity from the token only.
check "D3 portal login route"        grep -rq '"/api/v1/portal/login"' backend/internal/portalapi
check "D3 portal me route (GET+PUT)" grep -rq '"/api/v1/portal/me"' backend/internal/portalapi
check "D3 portal language route"     grep -rq '"/api/v1/portal/language"' backend/internal/portalapi
check "D3 portal usage route"        grep -rq '"/api/v1/portal/usage"' backend/internal/portalapi
check "D3 portal payments route"     grep -rq '"/api/v1/portal/payments"' backend/internal/portalapi
check "D3 portal voucher redeem route" grep -rq "vouchers/redeem" backend/internal/portalapi
check "D3 portal card-payments submit route" grep -rq '"/api/v1/portal/card-payments"' backend/internal/portalapi
check "D3 branding public route (C5)" grep -rq '"/api/v1/branding"' backend/internal/portalapi
check "D3 portalapi module mounted"  grep -q "internal/portalapi" backend/cmd/hikrad-api/modules.go

# Gateway layer (C3, FR-23): frozen interface, always-shipped mock adapter,
# first live adapter behind config-disabled-by-default, public callback +
# reconciliation worker + admin config API.
check "D3 PaymentGateway interface (C3)"  grep -q "type PaymentGateway interface" backend/internal/billing/gateways/gateway.go
check "D3 mock adapter (ships forever)"   test -f backend/internal/billing/gateways/mock/mock.go
check "D3 zaincash adapter (live, FR-23.5)" test -f backend/internal/billing/gateways/zaincash/zaincash.go
check "D3 gateway host docs (walled-garden input)" test -f backend/internal/billing/gateways/mock/README.md
check "D3 zaincash host docs (walled-garden input)" test -f backend/internal/billing/gateways/zaincash/README.md
check "D3 public callback route"          grep -rq "payments/{gateway}/callback" backend/internal/billing
check "D3 dev mock simulator route"       grep -rq "dev/mock-gateway/simulate" backend/internal/billing
check "D3 reconciliation worker"          grep -q "func (m \*Module) runReconciliation" backend/internal/billing/paymentintents.go
check "D3 gateway admin config route"     grep -rq "/api/v1/payment-gateways" backend/internal/billing
check "D3 gateway creds sealed with A's crypto" grep -q "crypto.Encrypt(plain)" backend/internal/billing/gatewaymgr.go

# Every renewal source converges on the single renewal path (C2 FR-19.3) —
# portal/gateway confirm and card-trial/approve go through m.renew, not a
# parallel writer.
check "D3 gateway confirm converges on m.renew" grep -q "m.renew(ctx, renewParams{" backend/internal/billing/paymentintents.go
check "D3 card trial/approve converge on m.renew" grep -q "m.renew(ctx, renewParams{" backend/internal/billing/cardpay.go

# billing.renewed (C7, FR-55) + billing.card_payment (C8) events — D publishes,
# C consumes (checked in C's own section above).
check "D3 billing.renewed publish (C7)"     grep -rq '"billing.renewed"' backend/internal/billing
check "D3 billing.card_payment publish (C8)" grep -rq '"billing.card_payment"' backend/internal/billing

# Scratch-card admin queue (C8, FR-59.2): verify permission-gated, reveal is
# a distinct audited action, codes never appear in the list query.
check "D3 card-payments admin queue route"  grep -rq '"/api/v1/card-payments"' backend/internal/billing
check "D3 card-payments reveal route"       grep -rq "card-payments/{id}/reveal" backend/internal/billing
check "D3 card-payments approve route"      grep -rq "card-payments/{id}/approve" backend/internal/billing
check "D3 card-payments reject route"       grep -rq "card-payments/{id}/reject" backend/internal/billing
check "D3 list query never selects card_code_enc" \
  sh -c "! awk '/func \(m \*Module\) listCardPayments/,/^}/' backend/internal/billing/cardpay.go | grep -q card_code_enc"
check "D3 reveal is audited"                grep -q "card_payment.reveal" backend/internal/billing/cardpay_api.go

# Full suites: token audience separation, /me composition (Decision 21 —
# scripted body-content check for forbidden quota fields), IDOR, voucher
# self-redeem, mock e-wallet full lifecycle incl. 3x-replay==1-renewal and a
# disabled-gateway graceful-degradation path (item 2); callback amount-
# mismatch/idempotent-replay and reconciliation-timing at the unexported
# level; card trial/approve-anchoring/reject-nets-zero/cooldown math (item
# 10). DB-backed cases self-skip without HIKRAD_TEST_DB_URL (+ …_REDIS_URL).
( cd backend && "$GO" test ./internal/billing/... >/dev/null 2>&1 ) \
  && pass "D3 billing suite (incl. gateway/card-payment math)" \
  || fail "D3 billing suite (incl. gateway/card-payment math)"
( cd backend && "$GO" test ./internal/portalapi/... >/dev/null 2>&1 ) \
  && pass "D3 portalapi suite (audience separation, IDOR, /me, e2e flows)" \
  || fail "D3 portalapi suite (audience separation, IDOR, /me, e2e flows)"
( cd backend && "$GO" test ./internal/subscribers/... >/dev/null 2>&1 ) \
  && pass "D3 subscribers suite (VerifyPassword/portal read-model unaffected)" \
  || fail "D3 subscribers suite (VerifyPassword/portal read-model unaffected)"
( cd backend && "$GO" build ./... >/dev/null 2>&1 ) \
  && pass "D3 backend builds cleanly (./...)" \
  || fail "D3 backend builds cleanly (./...)"

# ---------------------------------------------------------------------------
# Gate runner — named-scenario legs (phase amendment 2026-07-11: scriptable
# gate items 2, 6, 9-fake, 10 belong in this script). The suite-level legs
# above already execute these scenarios as part of `go test ./...`; the
# checks below pin each one to its specific test name so a regression in one
# gate-relevant scenario surfaces as its own line instead of being buried in
# a whole-package pass/fail.
# ---------------------------------------------------------------------------
echo "-- Gate runner: named scenario legs (items 2, 6, 9-fake, 10) --"

( cd backend && "$GO" test ./internal/portalapi/... -run '^TestMockGatewayLifecycleReplayAndDisabled$' -v 2>&1 | grep -q '^--- PASS' ) \
  && pass "item 2: mock-gateway lifecycle incl. 3x callback replay == 1 renewal + disabled-gateway graceful degradation" \
  || fail "item 2: mock-gateway lifecycle incl. 3x callback replay == 1 renewal + disabled-gateway graceful degradation"
( cd backend && "$GO" test ./internal/billing/... -run '^TestProcessCallbackReplayIdempotent$' -v 2>&1 | grep -q '^--- PASS' ) \
  && pass "item 2: callback replay idempotency at the unexported processCallback level" \
  || fail "item 2: callback replay idempotency at the unexported processCallback level"
( cd backend && "$GO" test ./internal/billing/... -run '^TestReconcileExpiresStaleIntent$' -v 2>&1 | grep -q '^--- PASS' ) \
  && pass "item 2: reconciliation worker expires a stuck-pending intent" \
  || fail "item 2: reconciliation worker expires a stuck-pending intent"

( cd backend && "$GO" test ./internal/portalapi/... -run '^TestPortalIDOR$' -v 2>&1 | grep -q '^--- PASS' ) \
  && pass "item 6: IDOR — scripted cross-subscriber read attempt fails" \
  || fail "item 6: IDOR — scripted cross-subscriber read attempt fails"
( cd backend && "$GO" test ./internal/portalapi/... -run '^TestPortalTokenAudienceSeparation$' -v 2>&1 | grep -q '^--- PASS' ) \
  && pass "item 6: IDOR — panel token rejected on portal routes and vice versa" \
  || fail "item 6: IDOR — panel token rejected on portal routes and vice versa"
( cd backend && "$GO" test ./internal/portalapi/... -run '^TestPortalLoginRateLimit$' -v 2>&1 | grep -q '^--- PASS' ) \
  && pass "item 6: portal login rate-limit — scripted brute-force lockout (NFR-4.6)" \
  || fail "item 6: portal login rate-limit — scripted brute-force lockout (NFR-4.6)"

( cd backend && "$GO" test ./internal/monitorsvc/... -run '^TestDeliverSubscriberWhatsApp_RequestCaptureFake$' -v 2>&1 | grep -q '^--- PASS' ) \
  && pass "item 9 (fake path): WhatsApp subscriber messaging proven against a request-capture fake" \
  || fail "item 9 (fake path): WhatsApp subscriber messaging proven against a request-capture fake"
( cd backend && "$GO" test ./internal/monitorsvc/... -run '^TestSubscriberEvents_RenewedDeliversWhatsAppReceipt$' -v 2>&1 | grep -q '^--- PASS' ) \
  && pass "item 9 (fake path): a completed renewal delivers the receipt template end to end" \
  || fail "item 9 (fake path): a completed renewal delivers the receipt template end to end"

( cd backend && "$GO" test ./internal/portalapi/... -run '^TestPortalCardPaymentSubmitAndQueue$' -v 2>&1 | grep -q '^--- PASS' ) \
  && pass "item 10: FR-59 API flow — submit creates a pending trial, admin queue lists it, code never leaks" \
  || fail "item 10: FR-59 API flow — submit creates a pending trial, admin queue lists it, code never leaks"
( cd backend && "$GO" test ./internal/billing/... -run '^TestCardTrialGuardsAndApproveAnchoring$' -v 2>&1 | grep -q '^--- PASS' ) \
  && pass "item 10: FR-59 approve math — expiry anchored at trial_started_at, one-pending guard" \
  || fail "item 10: FR-59 approve math — expiry anchored at trial_started_at, one-pending guard"
( cd backend && "$GO" test ./internal/billing/... -run '^TestCardRejectNetsZeroAndCooldown$' -v 2>&1 | grep -q '^--- PASS' ) \
  && pass "item 10: FR-59 reject math — net-zero reversing ledger entry + cooldown enforced" \
  || fail "item 10: FR-59 reject math — net-zero reversing ledger entry + cooldown enforced"

# ---------------------------------------------------------------------------
# (Other agents append their legs below this line.)
# ---------------------------------------------------------------------------

echo "== gate done =="
[ "$FAILED" -eq 0 ] || { echo "GATE: FAIL"; exit 1; }
echo "GATE: PASS"
