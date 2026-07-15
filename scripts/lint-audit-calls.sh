#!/usr/bin/env bash
# lint-audit-calls.sh — Phase-2 contract C2 guard (Agent 1, Platform & Security).
#
# Every mutating API endpoint in every backend module must write the audit log
# (FR-28.3): "every mutating endpoint in every module writes it". This is a
# grep-level check, not a proof — it flags domain modules that register a
# mutating route (POST/PUT/PATCH/DELETE under /api/v1) but never reference
# auth.Audit anywhere in the package, so a whole module can't silently ship
# without an audit trail. Fine-grained per-endpoint coverage is reviewed in PR.
#
# Usage: scripts/lint-audit-calls.sh   (run from repo root; CI-friendly, exits
# non-zero on a violation). Set AUDIT_LINT_DEBUG=1 for per-package detail.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
MODULES_DIR="$ROOT/backend/internal"

# Domain packages exempt from the rule:
#   auth      — owns Audit; its denial/login paths call AuditActor, and not
#               every route it exposes is a business mutation.
#   httpapi   — framework, registers no domain routes.
#   platform  — infra (config/db/settings), no /api/v1 mutations.
#   radius    — its only writer is the unauthenticated FreeRADIUS policy
#               endpoint (/internal/...), which has no manager actor to audit.
EXEMPT_REGEX='^(auth|httpapi|platform|radius)$'

status=0

# Iterate each Go package directory under backend/internal. A Go package is a
# single directory, so grep each dir's own *.go files non-recursively (a
# subdirectory is a different package, checked on its own iteration).
while IFS= read -r dir; do
    [ "$dir" = "$MODULES_DIR" ] && continue
    # Skip dirs with no Go source directly in them.
    compgen -G "$dir/*.go" >/dev/null 2>&1 || continue

    pkg="${dir#"$MODULES_DIR"/}"
    top="${pkg%%/*}"
    if [[ "$top" =~ $EXEMPT_REGEX ]]; then
        [ "${AUDIT_LINT_DEBUG:-0}" = "1" ] && echo "skip (exempt):    $pkg"
        continue
    fi

    # Does this package register a mutating /api/v1 route?
    if ! grep -hEq '\.(Post|Put|Patch|Delete)\(\s*"/api/v1' "$dir"/*.go 2>/dev/null; then
        [ "${AUDIT_LINT_DEBUG:-0}" = "1" ] && echo "ok (no mutations): $pkg"
        continue
    fi

    # If so, it must reference the audit writer somewhere in the package.
    if grep -hEq 'auth\.Audit(Actor)?\(' "$dir"/*.go 2>/dev/null; then
        [ "${AUDIT_LINT_DEBUG:-0}" = "1" ] && echo "ok (audited):     $pkg"
    else
        echo "AUDIT-LINT VIOLATION: package '$pkg' registers a mutating /api/v1 route but never calls auth.Audit" >&2
        status=1
    fi
done < <(find "$MODULES_DIR" -type d | sort)

if [ "$status" -eq 0 ]; then
    echo "audit-lint: all mutating modules reference auth.Audit"
fi
exit "$status"
