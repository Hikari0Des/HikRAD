# Phase 3 — Agent 1 (Platform & Security): roles matrix, TOTP 2FA, sessions, IP allowlist, audit viewer

> Owns FR-27 (complete), FR-28 (complete), FR-29 (complete), FR-30; NFR-4.5 groundwork. Depends on contracts in [00-phase.md](00-phase.md) (C1-A, C7); parallel with Agents 2–5.

## Mission & context
Phase 2 shipped hardcoded role sets; this phase completes sub-PRD [06-managers-roles-security](../../prd/06-managers-roles-security.md): editable roles with the full permission matrix, per-manager overrides, TOTP 2FA with backup codes and admin enforcement, complete panel-session management, per-manager IP allowlists, and the audit-log viewer API. You also start the ASVS L2 checklist that Phase 5 verifies.

## File ownership
- **Exclusive:** `backend/internal/auth/**`, `backend/migrations/0210_*.sql`–`0219_*.sql`, `docs/ops/security-checklist.md`.
- **Read-only:** everything else. **Forbidden:** `internal/{billing,radius,monitorsvc}`, `frontend/**`.

## Tasks
1. Migrations 0210–0219 per phase C1-A: `roles`, `role_permissions`, manager↔role, `ip_allowlists`, TOTP/backup-code fields. Migrate Phase-2 hardcoded roles into seeded editable rows (Admin/Operator/Agent per sub-PRD 06 FR-27.3) without breaking existing checks. [FR-27.1/27.3]
2. Permission engine v2: role permissions + per-manager overrides resolved to an effective set at login (embedded in the token/session, invalidated on role edit — force re-resolve within one access-token lifetime); same `Require`/`ScopeFilter` API, internals swapped. Deny-by-default preserved; document the Phase-2 → 3 migration in the package README. [FR-27]
3. Roles API per C7: CRUD roles + matrix, assign to managers, per-manager overrides; deleting an in-use role blocked (reassign first). Audit everything. [FR-27.1]
4. TOTP per C7: enroll (QR otpauth URI + manual key), verify-activate, disable (password + code), 10 hashed backup codes (one-time), admin "require 2FA" (global/per-role setting) blocking login until enrolled, admin reset of a locked-out manager (audited). Login flow gains the TOTP step when active. [FR-28.1]
5. IP allowlist: CIDR list per manager, enforced at login and per-request (middleware); empty = unrestricted; self-edit warning data (current IP excluded?) in the API response for E. [FR-30]
6. Sessions complete: admin view/revoke any manager's sessions per C7; revocation effective ≤ access-token TTL (5 min); password change/2FA reset revoke others. [FR-29]
7. Audit viewer API: full filters (actor, entity, action, date range), human-readable action summaries precomputed server-side (localizable keys, not prose), CSV export under `export` permission. [FR-28.3]
8. `docs/ops/security-checklist.md`: ASVS L2 checklist skeleton mapped to HikRAD surfaces (verified pass is Phase 5). [NFR-4.5]

Edge cases: role edit must not grant more than the editor holds (privilege-escalation guard); TOTP clock skew ±1 step; backup code reuse rejected; allowlist + proxy X-Forwarded-For handling (trust only Caddy); audit summaries must redact sealed fields.

## Contracts consumed/exposed
- **Consumes:** Phase-2 auth base (own), settings (2FA enforcement flag).
- **Exposes:** C7 security endpoints (E builds UIs on them); unchanged `Require`/`ScopeFilter`/`Audit` signatures for all agents.

## Definition of done
- Gate item 5 passes in full; permission-resolution swap breaks no Phase-2 test (whole suite green).
- Tests: matrix resolution incl. overrides, escalation guard, TOTP flows (enroll/verify/skew/backup/disable/admin-reset), allowlist enforcement incl. XFF, revocation timing, audit filter queries + redaction.

## Handoff
Phase 5 (same role) runs the ASVS verification against this module; E ships the matching UIs this phase; other agents keep coding against unchanged auth APIs.
