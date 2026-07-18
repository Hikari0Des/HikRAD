#!/bin/sh
# gate-v2-phase-12.sh — machine-checkable legs of the v2 phase-12 integration
# gate (docs/v2/phases/phase-v2-12-frontend-modernization/00-phase.md). Each
# check prints a label followed by PASS or FAIL; exit status is non-zero if
# any leg fails. Frontend-only phase: no DB/Redis dependency anywhere. Human
# leg (the 360/768/1280 x light/dark x LTR/RTL manual matrix) is recorded in
# gate-result.md, not scripted here.
#
# This script is written at kickoff time, before any feature code exists —
# same convention as every prior v2 phase (see docs/ops/known-issues.md,
# 2026-07-18 "Tooling / scripts/gate-v2-phase-*.sh check_go_test" row for the
# precedent and its caveat). Every leg below will legitimately FAIL until the
# phase's own implementation commits land; that is the point of freezing the
# gate before the code, not a bug in this script.
#
# Usage:  sh scripts/gate-v2-phase-12.sh
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

echo "== v2 phase 12 gate (frontend modernization) =="

# --- Item 1: no schema change -------------------------------------------------
echo "-- No schema change (frontend-only phase) --"
check "backend builds (untouched by this phase)" "$GO" -C backend build ./...
check "backend vets (untouched by this phase)" "$GO" -C backend vet ./...

# --- Item 2: no-native-controls grep gate (FR-94.3, C4) -----------------------
echo "-- No-native-controls CI gate --"
check "lint-no-native-controls.sh exists" test -f scripts/lint-no-native-controls.sh
check "no bare native select/checkbox/radio outside the control library (panel)" \
  sh -c 'sh scripts/lint-no-native-controls.sh panel'
check "no bare native select/checkbox/radio outside the control library (portal)" \
  sh -c 'sh scripts/lint-no-native-controls.sh portal'

# --- Item 3: new control set exists and is tested (C1-C3) ---------------------
echo "-- New control set (panel) --"
for ctrl in Select Combobox TextInput Textarea Checkbox Radio Switch FileInput; do
  check "panel components/form/${ctrl}.test.tsx present and green" \
    sh -c "cd frontend/panel && npx vitest run src/components/form/${ctrl}.test.tsx"
done

# --- Item 4: panel adoption complete (C5) --------------------------------------
echo "-- Panel adoption --"
check "NasScopePicker test green against the new control set" \
  sh -c 'cd frontend/panel && npx vitest run src/components/NasScopePicker.test.tsx'
check "SubscriberFormModal test green" \
  sh -c 'cd frontend/panel && npx vitest run src/pages/subscribers/SubscriberFormModal.test.tsx'
check "RoleMatrix test green" \
  sh -c 'cd frontend/panel && npx vitest run src/pages/security/RoleMatrix.test.tsx'

# --- Item 5: NasScopePicker behavior-preserving (C6) ---------------------------
echo "-- NasScopePicker behavior-preserving (C6) --"
check "NasScopePicker pure-helper behavior unchanged (chip add/remove, any-NAS default, whole-vs-service narrowing)" \
  sh -c 'cd frontend/panel && npx vitest run src/components/NasScopePicker.test.tsx -t "toggleScope"'

# --- Item 6: portal adoption ----------------------------------------------------
echo "-- Portal adoption --"
check "portal has the new Radix dependencies" \
  sh -c 'grep -q "@radix-ui/react-select" frontend/portal/package.json'
check "portal components/form/ exists" test -d frontend/portal/src/components/form
check "portal control-set component test green" \
  sh -c 'cd frontend/portal && npx vitest run src/components/form'

# --- Item 7: responsive smoke test (FR-95, C7) ----------------------------------
echo "-- Responsive/overflow smoke test (360/768/1280) --"
check "panel layoutSmoke test green" \
  sh -c 'cd frontend/panel && npx vitest run src/layoutSmoke.test.tsx'
check "portal layoutSmoke test green" \
  sh -c 'cd frontend/portal && npx vitest run src/layoutSmoke.test.tsx'

# --- Item 8: known-issues row closed (FR-95.1) ----------------------------------
echo "-- known-issues.md layout row closed --"
check "2026-07-17 layout row no longer says Open" \
  sh -c '! grep "2026-07-17 | Panel+portal / layout" docs/ops/known-issues.md | grep -q "| Open —"'

# --- Item 9: panel stylelint adoption (C9) --------------------------------------
echo "-- Panel stylelint adoption --"
check "frontend/panel/stylelint.config.mjs exists" test -f frontend/panel/stylelint.config.mjs
check "panel lint script runs stylelint" \
  sh -c 'grep -q "stylelint" frontend/panel/package.json'
check "panel stylelint passes" sh -c 'cd frontend/panel && npx stylelint "src/**/*.css"'

# --- Item 10: focus rings present (C8) ------------------------------------------
echo "-- Focus rings (C8) --"
check "panel focus-visible test green" \
  sh -c 'cd frontend/panel && npx vitest run src/components/form/focusRing.test.tsx'

# --- Item 11: reduced motion respected (C8) -------------------------------------
echo "-- Reduced motion (C8) --"
check "no unguarded new transition (motion-reduce/prefers-reduced-motion present wherever a transition was added)" \
  sh -c 'grep -rl "motion-reduce:\|prefers-reduced-motion" frontend/panel/src/components/form frontend/portal/src/components/form 2>/dev/null | grep -q .'

# --- Item 12: panel/portal build/lint/i18n --------------------------------------
echo "-- Panel/portal build/lint/i18n --"
check "shared build"  sh -c 'cd frontend && npm run build --workspace=@hikrad/shared'
check "panel build"   sh -c 'cd frontend/panel && npm run build'
check "panel lint"    sh -c 'cd frontend/panel && npm run lint'
check "portal build"  sh -c 'cd frontend/portal && npm run build'
check "portal lint"   sh -c 'cd frontend/portal && npm run lint'
check "i18n:check (0 missing keys / 0 hardcoded strings)" sh -c 'cd frontend && node shared/scripts/i18n-check.mjs'

# --- Item 13: full regression -----------------------------------------------------
echo "-- Full regression --"
check "panel full vitest suite green" sh -c 'cd frontend/panel && npx vitest run'
check "portal full vitest suite green" sh -c 'cd frontend/portal && npx vitest run'
check "shared full vitest suite green" sh -c 'cd frontend/shared && npx vitest run'

# --- Item 14: docs accuracy -------------------------------------------------------
echo "-- Docs --"
check "PRD carries FR-94, FR-95, and FR-96" \
  sh -c 'grep -q "FR-94" docs/PRD.md && grep -q "FR-95" docs/PRD.md && grep -q "FR-96" docs/PRD.md'
check "sub-PRD 07 carries FR-94, FR-95, and FR-96" \
  sh -c 'grep -q "FR-94" docs/prd/07-subscriber-portal-pwa.md && grep -q "FR-95" docs/prd/07-subscriber-portal-pwa.md && grep -q "FR-96" docs/prd/07-subscriber-portal-pwa.md'
check "00-index.md shows 96/96 FRs owned" \
  sh -c 'grep -q "96/96 owned" docs/prd/00-index.md'
check "phase brief present" test -f docs/v2/phases/phase-v2-12-frontend-modernization/00-phase.md

echo
if [ "$FAILED" -eq 0 ]; then
  echo "== v2 phase 12 gate: ALL SCRIPTED LEGS PASSED =="
else
  echo "== v2 phase 12 gate: FAILURES ABOVE (expected until this phase's implementation commits land) =="
fi
exit "$FAILED"
