#!/bin/sh
# gate-v2-phase-7.sh — machine-checkable legs of the v2 phase-7 integration
# gate (docs/v2/phases/phase-v2-7-one-click-update/00-phase.md). Each check
# prints a label followed by PASS or FAIL; exit status is non-zero if any
# leg fails.
#
# Two legs (items 11-12 in the phase doc: clean-VM update via button,
# broken-image autonomous rollback with the panel dead) are human/hardware
# legs this script cannot exercise — they are recorded as documented-pending
# in gate-result.md, same sanctioned pattern as v2-5's own gate.
#
# Written at kickoff, before feature code exists — most legs below guard on
# "not yet implemented" until the daemon/relay/panel land, same convention
# v2-5's and v2-6's gate scripts used at their own kickoff.
#
# Usage:  sh scripts/gate-v2-phase-7.sh
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

GO=go
command -v go >/dev/null 2>&1 || {
  for c in "/c/Program Files/Go/bin/go" "/usr/local/go/bin/go"; do
    [ -x "$c" ] && GO=$c && break
  done
}

echo "== v2 phase 7 gate (one-click update from the panel) =="

# --- Item 1: build & syntax -------------------------------------------------
echo "-- Build health --"
check "backend builds" "$GO" -C backend build ./...
check "backend vets" "$GO" -C backend vet ./...
check "install.sh still syntactically valid" bash -n scripts/install.sh
check "hikrad still syntactically valid" bash -n scripts/hikrad
if [ -f scripts/hikrad-updaterd ]; then
  check "scripts/hikrad-updaterd is syntactically valid" bash -n scripts/hikrad-updaterd
fi

# --- Item 2: socket protocol — verbs only, token-gated (C2) ----------------
echo "-- Daemon socket protocol (C1/C2) --"
if [ -d backend/cmd/hikrad-updaterd ]; then
  pass "backend/cmd/hikrad-updaterd present"
  check_go_test() {
    label=$1; shift
    out=$("$@" -v 2>&1)
    case "$out" in
      *FAIL*) fail "$label" ;;
      *PASS*) pass "$label" ;;
      *)      fail "$label" ;;
    esac
  }
  check_go_test "[leg] daemon rejects a missing/wrong token before touching the lock" \
    "$GO" -C backend test ./cmd/hikrad-updaterd/... -run TestUnauthorizedRefused
  check_go_test "[leg] daemon rejects a bundle_path outside incoming/ or the wrong filename shape" \
    "$GO" -C backend test ./cmd/hikrad-updaterd/... -run TestBundlePathValidation
else
  fail "backend/cmd/hikrad-updaterd present (not yet implemented)"
fi

# --- Item 3: lock semantics (C3) --------------------------------------------
echo "-- Lock semantics (C3) --"
if [ -d backend/cmd/hikrad-updaterd ]; then
  check_go_test "[leg] concurrent update requests: exactly one proceeds, the other is 'locked'" \
    "$GO" -C backend test ./cmd/hikrad-updaterd/... -run TestConcurrentUpdateLock
  check "hikrad's cmd_update acquires the same lock file path the daemon uses" \
    grep -q "update.lock" scripts/hikrad
else
  fail "lock-sharing test present (daemon not yet implemented)"
fi

# --- Item 4: no shell-reachable arguments (FR-88.1) -------------------------
echo "-- No argument ever reaches a shell (FR-88.1) --"
if [ -d backend/cmd/hikrad-updaterd ]; then
  check "no sh/bash -c or string-built exec.Command in the daemon (must find NONE -> leg PASSES)" \
    sh -c '! grep -rnE "exec\.Command\(\"(sh|bash)\"|exec\.Command\(.*\+.*\)" backend/cmd/hikrad-updaterd/'
else
  fail "grep leg present (daemon not yet implemented)"
fi

# --- Item 5: autonomous rollback survives the daemon (FR-86.5/88.3) --------
echo "-- Rollback survives the daemon dying (FR-86.5/88.3) --"
if [ -d backend/cmd/hikrad-updaterd ]; then
  check_go_test "[leg] child hikrad-update process completes rollback after the daemon is killed" \
    "$GO" -C backend test ./cmd/hikrad-updaterd/... -run TestRollbackSurvivesDaemonDeath
  check_go_test "[leg] a restarted daemon's rollback-status reflects the on-disk state file" \
    "$GO" -C backend test ./cmd/hikrad-updaterd/... -run TestRollbackStatusPersistsAcrossRestart
else
  fail "rollback-survival test present (daemon not yet implemented)"
fi

# --- Item 6: permission gate (C5) -------------------------------------------
echo "-- system.update permission gate (C5) --"
if [ -d backend/internal/updates ]; then
  pass "backend/internal/updates present"
  check "internal/updates registers with the module registry" \
    grep -rq "httpapi.Add" backend/internal/updates
  check "all four routes require system.update" \
    sh -c '[ "$(grep -rc "system.update" backend/internal/updates/*.go | awk -F: "{s+=\$2} END{print s}")" -ge 4 ]'
  if [ -n "${HIKRAD_TEST_DB_URL:-}" ] && [ -n "${HIKRAD_TEST_REDIS_URL:-}" ]; then
    check_go_test "[DB] non-admin without system.update gets 403 on all four routes" \
      "$GO" -C backend test ./internal/updates/... -run TestPermissionGate_Denied
    check_go_test "[DB] admin and a custom role granted system.update both pass" \
      "$GO" -C backend test ./internal/updates/... -run TestPermissionGate_Allowed
  else
    echo "  [SKIP] DB-gated permission tests (HIKRAD_TEST_DB_URL/HIKRAD_TEST_REDIS_URL unset)"
  fi
else
  fail "backend/internal/updates present (not yet implemented)"
fi

# --- Item 7: audit logging (C4) ---------------------------------------------
echo "-- Audit logging (C4) --"
if [ -d backend/internal/updates ]; then
  check "POST /system/update calls auth.Audit" \
    grep -rq "auth.Audit" backend/internal/updates
else
  fail "audit-logging call site present (not yet implemented)"
fi

# --- Item 8: SSE relay shape (C4) -------------------------------------------
echo "-- SSE relay shape (C4) --"
if [ -d backend/internal/updates ]; then
  check_go_test "[leg] SSE stream emits progress/done/rolled_back frames matching encodeSSE framing" \
    "$GO" -C backend test ./internal/updates/... -run TestSSERelayShape
else
  fail "SSE relay test present (not yet implemented)"
fi

# --- Item 9: panel/build -----------------------------------------------------
echo "-- Panel --"
if grep -qE "['\"]system\.update['\"]" frontend/panel/src/pages/settings/SystemSettings.tsx 2>/dev/null; then
  pass "SystemSettings.tsx references the system.update permission gate"
else
  fail "SystemSettings.tsx wires the update buttons behind system.update (not yet implemented)"
fi

# --- Item 10: docs accuracy --------------------------------------------------
echo "-- Docs --"
check "PRD carries FR-86..FR-88" \
  sh -c 'grep -q "FR-86" docs/PRD.md && grep -q "FR-87" docs/PRD.md && grep -q "FR-88" docs/PRD.md'
check "phase brief present" test -f docs/v2/phases/phase-v2-7-one-click-update/00-phase.md
check "known-issues.md no longer mislabels this feature as v2 phase 5" \
  sh -c '! grep -q "Planned: v2 phase 5" docs/ops/known-issues.md'
check "known-issues.md carries a v2 phase 7 / FR-86 reference" \
  grep -qi "v2 phase 7\|FR-86" docs/ops/known-issues.md
if grep -qi "hikrad-updaterd" docs/ops/update.md 2>/dev/null; then
  pass "update.md describes the panel-triggered path"
else
  fail "update.md describes the panel-triggered path (not yet updated — expected until feature code lands)"
fi

echo
echo "== Human/hardware legs (not scriptable — record status in gate-result.md) =="
echo "  [ ] Clean-VM update via button (item 11): real install, admin clicks 'Update now',"
echo "      panel reloads on the new version after reconnecting, pre-update backup exists."
echo "  [ ] Broken-image autonomous rollback, panel dead throughout (item 12): same VM, a bundle"
echo "      engineered to fail health-check, panel's own container killed mid-update, rollback"
echo "      still completes and is correctly reported on the panel's next reconnect."

echo
if [ "$FAILED" -eq 0 ]; then
  echo "== v2 phase 7 gate: ALL SCRIPTED LEGS PASSED =="
else
  echo "== v2 phase 7 gate: FAILURES ABOVE =="
fi
exit "$FAILED"
