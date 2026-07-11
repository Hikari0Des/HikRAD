#!/bin/sh
# gate-phase-3.sh — machine-checkable legs of the Phase-3 integration gate
# (docs/phases/phase-3-billing-security-monitoring/00-phase.md §Execution
# efficiency). Each agent appends its own legs below; every check prints a
# label followed by PASS or FAIL. Exit status is non-zero if any leg fails.
#
# Physical/UX legs (gate items 1, 4, 6) are human-run and are NOT scripted here.
#
# Usage:  sh scripts/gate-phase-3.sh
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

echo "== Phase 3 gate =="

# ---------------------------------------------------------------------------
# Agent 1 — Platform & Security (gate item 5: security module)
# ---------------------------------------------------------------------------
echo "-- Agent 1: Platform & Security --"

# C1-A schema present.
for f in 0210_roles 0211_manager_overrides 0212_ip_allowlist 0213_totp_backup_codes; do
  check "A1 migration $f present" test -f "backend/migrations/${f}.up.sql"
done

# Builtin roles seeded as editable rows (FR-27.3) mirroring the Phase-2 sets.
check "A1 builtin roles seeded in 0210" grep -q "INSERT INTO roles" backend/migrations/0210_roles.up.sql

# audit_log immutability (AC-28c): DB-level trigger + REVOKE.
check "A1 audit_log immutable trigger" grep -q "audit_log_immutable" backend/migrations/0112_audit_log.up.sql
check "A1 audit_log REVOKE UPDATE/DELETE" grep -q "REVOKE UPDATE, DELETE ON audit_log" backend/migrations/0112_audit_log.up.sql

# Vendor-neutral permission checks: no role-name authorization outside the
# permission model (frozen contract — code checks permission strings only).
check "A1 no role-name authz in domain modules" sh -c '! grep -rniE "role *== *\"(admin|operator|agent)\"" backend/internal/subscribers backend/internal/profiles backend/internal/radius backend/internal/live 2>/dev/null'

# Security module test suite: matrix resolution incl. overrides, escalation
# guard, TOTP flows, allowlist incl. XFF, revocation, audit filters + redaction.
# (DB-backed cases self-skip unless HIKRAD_TEST_DB_URL is set.)
if [ -d backend/internal/auth ]; then
  ( cd backend && "$GO" test ./internal/auth/... >/dev/null 2>&1 ) \
    && pass "A1 auth suite (go test ./internal/auth/...)" \
    || fail "A1 auth suite (go test ./internal/auth/...)"
fi

# ---------------------------------------------------------------------------
# Agent 2 — RADIUS & NAS (gate item 4: runtime enforcement; item 7: debug tool
# backend; FR-11 TOD sweeps; FR-18 hotspot template)
# ---------------------------------------------------------------------------
echo "-- Agent 2: RADIUS & NAS --"

# C1-B / 0220: enforcement worker state + idempotency cursor table.
check "B2 migration 0220_enforcement present" test -f backend/migrations/0220_enforcement.up.sql
check "B2 enforcement dedup unique index (idempotency)" \
  grep -q "enforcement_actions_dedup_idx" backend/migrations/0220_enforcement.up.sql

# FR-9/FR-10 runtime enforcement worker consumes the frozen C4 channels.
check "B2 worker subscribes enforce.quota_exceeded" grep -rq "enforce.quota_exceeded" backend/internal/radius
check "B2 worker subscribes enforce.expired" grep -rq "enforce.expired" backend/internal/radius
# enforcement_failures counter exposed for C's health.
check "B2 enforcement_failures counter exposed" grep -rq "enforce:failures" backend/internal/radius

# FR-39 debug tool backend: SSE over radius:decisions, nas.view-gated (item 7).
check "B2 debug SSE route /api/v1/live/debug" grep -rq "/api/v1/live/debug" backend/internal/radius

# FR-11 time-of-day sweeps publish tod.window for C's exemption marking.
check "B2 tod.window publish channel" grep -rq "tod.window" backend/internal/radius

# FR-18 hotspot login package endpoint.
check "B2 hotspot-package route" grep -rq "hotspot-package" backend/internal/radius

# FR-17 vendor neutrality still holds (burst syntax only in the adapter).
if [ -f scripts/lint-vendor-isolation.sh ]; then
  # bash, not sh: the script's own shebang is bash and it uses bash-only
  # syntax (`set -o pipefail`, BASH_SOURCE) that a POSIX /bin/sh (e.g. dash)
  # rejects outright, which previously read as a false vendor-isolation FAIL.
  check "B2 vendor isolation (no Mikrotik VSA in core)" bash scripts/lint-vendor-isolation.sh
fi

# RADIUS & NAS suite: enforcement matrix/idempotency/NAK-fallback, TOD boundary
# math (Asia/Baghdad), burst composition, voucher-format detection, debug filter.
( cd backend && "$GO" test ./internal/radius/... >/dev/null 2>&1 ) \
  && pass "B2 radius suite (go test ./internal/radius/...)" \
  || fail "B2 radius suite (go test ./internal/radius/...)"

# ---------------------------------------------------------------------------
# Agent 3 — Accounting & Monitoring (gate item 6: NAS down→alert + recovery;
# item 7: dashboard/health correctness; item 8: monitored-device probe check)
# ---------------------------------------------------------------------------
echo "-- Agent 3: Accounting & Monitoring --"

# C1-C / 0230–0234: probe + alert + device + sparkline schema present.
for f in 0230_health_probes 0231_alert_rules 0232_alert_events 0233_monitored_devices 0234_online_samples; do
  check "C3 migration $f present" test -f "backend/migrations/${f}.up.sql"
done

# health_probes is a hypertable with the one-target (nas XOR device) constraint.
check "C3 health_probes hypertable" grep -q "create_hypertable('health_probes'" backend/migrations/0230_health_probes.up.sql
check "C3 health_probes one-target constraint" grep -q "health_probes_one_target" backend/migrations/0230_health_probes.up.sql

# Frozen rule types incl. the FR-60 device_down|device_up additions (C5).
check "C3 device_down|device_up rule types (schema)" grep -Eq "device_down.*device_up|device_up" backend/migrations/0231_alert_rules.up.sql
check "C3 device_* rule types (validation)" grep -q "device_down" backend/internal/monitorsvc/rules_api.go

# Default rule set seeded (NAS down, disk 85%, backlog, invariant broken).
check "C3 default alert rules seeded" grep -q "INSERT INTO alert_rules" backend/migrations/0231_alert_rules.up.sql

# FR-60: monitored_devices is a SEPARATE table (never a FreeRADIUS client / NAS).
# The probe engine may READ the nas table (SELECT, as a target list), but the
# monitoring code must never WRITE the NAS registry or the FreeRADIUS clients file.
check "C3 monitored_devices table" grep -q "CREATE TABLE monitored_devices" backend/migrations/0233_monitored_devices.up.sql
check "C3 devices never write nas registry / clients.conf" \
  sh -c '! grep -rniqE "INSERT INTO nas|UPDATE nas |DELETE FROM nas|clients\.conf" backend/internal/monitorsvc'

# C5 API surface mounted (dashboard, health, devices CRUD, alert rules/events,
# probe history, in-app notifications SSE).
check "C3 dashboard route (FR-32)"        grep -rq "/api/v1/dashboard" backend/internal/monitorsvc
check "C3 health route (FR-35/40)"        grep -rq "/api/v1/health" backend/internal/monitorsvc
check "C3 devices CRUD route (FR-60)"     grep -rq "/api/v1/devices" backend/internal/monitorsvc
check "C3 alert-rules route (FR-36)"      grep -rq "/api/v1/alert-rules" backend/internal/monitorsvc
check "C3 alert-events route"             grep -rq "/api/v1/alert-events" backend/internal/monitorsvc
check "C3 notifications SSE route"        grep -rq "/api/v1/live/notifications" backend/internal/monitorsvc
check "C3 device probe-history route"     grep -rq "devices/{id}/probes" backend/internal/monitorsvc
check "C3 monitorsvc module mounted"      grep -q "internal/monitorsvc" backend/cmd/hikrad-api/modules.go

# Gate item 6 plumbing: NAS recovery publishes nas.recovered (C5) and the four
# channels (incl. whatsapp, Decision 16) exist with quiet-hours suppression.
check "C3 nas.recovered publish"          grep -rq "nas.recovered" backend/internal/monitorsvc
check "C3 whatsapp channel present"       grep -rq "whatsapp" backend/internal/monitorsvc/channels.go
check "C3 quiet hours suppression"        grep -rq "inQuietHours" backend/internal/monitorsvc

# hikrad-monitor binary + compose block wired.
check "C3 hikrad-monitor binary"          test -f backend/cmd/hikrad-monitor/main.go
check "C3 monitor compose block"          grep -q "hikrad-monitor:" deploy/compose.yml
check "C3 monitor Dockerfile"             test -f deploy/docker/monitor.Dockerfile

# Item 8 device probe check + item 6/7 logic: probe state machine (flap/recovery),
# rule cooldown/quiet-hours matrix, channel failure isolation, SNMP codec,
# downtime reconstruction. (DB-backed cases self-skip unless HIKRAD_TEST_DB_URL.)
( cd backend && "$GO" test ./internal/monitorsvc/... >/dev/null 2>&1 ) \
  && pass "C3 monitorsvc suite (go test ./internal/monitorsvc/...)" \
  || fail "C3 monitorsvc suite (go test ./internal/monitorsvc/...)"

# Accounting pipeline still green (FR-37/38/40 — the invariant C's health surfaces).
( cd backend && "$GO" test ./internal/accounting/... ./internal/live/... >/dev/null 2>&1 ) \
  && pass "C3 accounting+live suite" \
  || fail "C3 accounting+live suite"

# ---------------------------------------------------------------------------
# Agent 4 — Backend Business (gate items 1-3 scriptable halves: renew+ledger,
# balance blocking, race-proof voucher; item 5 ledger immutability).
# ---------------------------------------------------------------------------
echo "-- Agent 4: Backend Business --"

# C1-D schema present (0200-0204, exclusive range).
for f in 0200_ledger 0201_payments 0202_vouchers 0203_profile_burst_tod 0204_renewal_idempotency; do
  check "D migration $f present" test -f "backend/migrations/${f}.up.sql"
done

# Money-table immutability at the DB level (AC-24a): trigger + REVOKE, mirroring
# audit_log. Balances are ledger-DERIVED, never a stored-and-edited field.
check "D ledger append-only trigger"      grep -q "ledger_immutable" backend/migrations/0200_ledger.up.sql
check "D ledger REVOKE UPDATE/DELETE"     grep -q "REVOKE UPDATE, DELETE ON ledger_transactions" backend/migrations/0200_ledger.up.sql
check "D payments append-only trigger"    grep -q "payments_immutable" backend/migrations/0201_payments.up.sql
check "D frozen revenue_daily view"       grep -q "CREATE VIEW revenue_daily" backend/migrations/0201_payments.up.sql

# The single money path + C2/C3 API surface (E consumes these).
check "D renewal route (C2 FR-19)"        grep -rq "subscribers/{id}/renew" backend/internal/billing
check "D refund route (FR-25)"            grep -rq "subscribers/{id}/refund" backend/internal/billing
check "D balance route (FR-20)"           grep -rq "managers/{id}/balance" backend/internal/billing
check "D topup route (FR-20)"             grep -rq "managers/{id}/topup" backend/internal/billing
check "D voucher batch route (FR-22)"     grep -rq "vouchers/batches" backend/internal/billing
check "D voucher redeem route (FR-22)"    grep -rq "vouchers/redeem" backend/internal/billing
check "D receipt route (FR-21)"           grep -rq "payments/{receipt_no}/receipt" backend/internal/billing
check "D ledger list+export (FR-24)"      grep -rq "/api/v1/ledger" backend/internal/billing
check "D internal subscriber stats"       grep -rq "/internal/stats/subscribers" backend/internal/billing
check "D billing module mounted"          grep -q "internal/billing" backend/cmd/hikrad-api/modules.go

# Single-path discipline: every renewal source converges on renewInTx (panel,
# voucher redeem) — no other writer extends expiry / bills a subscriber.
check "D renewal converges on renewInTx"  grep -rq "renewInTx" backend/internal/billing/voucher.go

# Cross-agent seams D wires: burst fields into B's AuthView, TOD provider for B's
# sweeps, enforce.expired publish from the expiry sweep (C4).
check "D burst fields into AuthView (C4)" grep -rq "BurstRate" backend/internal/subscribers/authview.go
check "D TOD provider wired (C4/FR-11)"   grep -rq "SetTODProvider" backend/internal/profiles
check "D enforce.expired publish (C4)"    grep -rq "enforce.expired" backend/internal/subscribers/sweep.go

# Billing test suite: anchor matrix + price resolution + refund math + receipt
# rendering + voucher entropy (always run); balance≡ledger property, 50-goroutine
# voucher double-redeem storm, ledger immutability (DB-backed, self-skip w/o env).
( cd backend && "$GO" test ./internal/billing/... >/dev/null 2>&1 ) \
  && pass "D billing suite (go test ./internal/billing/...)" \
  || fail "D billing suite (go test ./internal/billing/...)"

# Business read-models still green after the burst/TOD schema + AuthView changes.
( cd backend && "$GO" test ./internal/subscribers/... ./internal/profiles/... >/dev/null 2>&1 ) \
  && pass "D subscribers+profiles suite" \
  || fail "D subscribers+profiles suite"

# ---------------------------------------------------------------------------
# Agent 5 — Frontend Panel (gate items 1-3, 5-7 UI halves: renew dialog,
# balances/ledger/refund, dashboard, monitoring, security + roles/2FA UIs).
# ---------------------------------------------------------------------------
echo "-- Agent 5: Frontend Panel --"

# API clients bind the frozen C2/C3/C5/C6/C7 contracts.
check "E5 billing API client (C2/C3)"     test -f frontend/panel/src/api/billing.ts
check "E5 monitoring API client (C5)"     test -f frontend/panel/src/api/monitoring.ts
check "E5 security API client (C7)"       test -f frontend/panel/src/api/security.ts
check "E5 debug SSE client (C6)"          test -f frontend/panel/src/api/debug.ts

# Hero flow + key screens present (gate items 1-3, 5-7 UI halves).
check "E5 renew dialog (FR-19, item 1)"   test -f frontend/panel/src/pages/subscribers/RenewModal.tsx
check "E5 ledger + refund (FR-24/25)"     test -f frontend/panel/src/pages/billing/LedgerPage.tsx
check "E5 vouchers (FR-22)"               test -f frontend/panel/src/pages/billing/VouchersPage.tsx
check "E5 dashboard (FR-32, item 5)"      grep -q "getDashboard" frontend/panel/src/pages/DashboardPage.tsx
check "E5 devices CRUD (FR-60)"           test -f frontend/panel/src/pages/monitoring/DevicesPage.tsx
check "E5 health + invariant badge"       grep -q "invariant" frontend/panel/src/pages/monitoring/HealthPage.tsx
check "E5 alerts rules+events (FR-36)"    test -f frontend/panel/src/pages/monitoring/AlertsPage.tsx
check "E5 roles matrix (FR-27)"           test -f frontend/panel/src/pages/security/RoleMatrix.tsx
check "E5 TOTP enrol (FR-28)"             test -f frontend/panel/src/pages/security/TotpEnroll.tsx
check "E5 debug tail UI (FR-39, item 7)"  test -f frontend/panel/src/pages/radius/DebugPage.tsx

# Renew feature slot activated (Phase-2 disabled placeholder retired).
check "E5 renew feature flag on"          grep -q "renew: true" frontend/panel/src/config/features.ts

# Machine-checkable UI gate: type-check + build, unit tests, no hardcoded
# user-visible strings (en/ar/ku parity). Run from the frontend workspace root.
( cd frontend/panel && npm run lint >/dev/null 2>&1 ) \
  && pass "E5 panel lint (eslint + prettier)" \
  || fail "E5 panel lint (eslint + prettier)"
( cd frontend/panel && npm run build >/dev/null 2>&1 ) \
  && pass "E5 panel build (tsc -b && vite build)" \
  || fail "E5 panel build (tsc -b && vite build)"
( cd frontend/panel && npx vitest run >/dev/null 2>&1 ) \
  && pass "E5 panel tests (vitest run)" \
  || fail "E5 panel tests (vitest run)"
( cd frontend && npm run i18n:check >/dev/null 2>&1 ) \
  && pass "E5 i18n:check (no hardcoded strings, en/ar/ku parity)" \
  || fail "E5 i18n:check (no hardcoded strings, en/ar/ku parity)"

# ---------------------------------------------------------------------------
# (Other agents append their legs below this line.)
# ---------------------------------------------------------------------------

echo "== gate done =="
[ "$FAILED" -eq 0 ] || { echo "GATE: FAIL"; exit 1; }
echo "GATE: PASS"
