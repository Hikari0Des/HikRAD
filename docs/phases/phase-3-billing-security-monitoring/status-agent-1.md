# Phase 3 — Agent 1 (Platform & Security) status

**Built (FR-27/28/29/30, NFR-4.5):**
- Migrations `0210–0213`: `roles`+`role_permissions` (wildcard `*`), `managers.role_id`, `manager_permission_overrides`, `manager_ip_allowlist`, TOTP pending secret + `manager_backup_codes`. 0210 seeds Admin/Operator/Agent as editable rows mirroring the Phase-2 sets and backfills `role_id` — behaviour-preserving swap.
- Permission engine v2: `resolvePermissions` (role + overrides) resolved at login/refresh, embedded in the access token (`perms` claim); `Manager.Can` reads it (wildcard aware) with a builtin-role fallback for unresolved Managers. Role/override edits take effect ≤ one access-token TTL. Migration documented in `internal/auth/README.md`.
- Roles API (C7): CRUD + matrix, `GET /permissions` catalog, privilege-escalation guard, in-use/builtin delete block (409). All audited.
- TOTP (RFC 6238, ±1 skew): enroll→verify(+10 one-time backup codes)→disable; admin reset; login TOTP step; global (`security.require_2fa`) / per-role enforcement via a limited enrolment-grant token.
- IP allowlist (FR-30): per-manager CIDRs enforced at login and every request (embedded `ips` claim); self-edit warning (`current_ip_allowed`) in the API.
- Sessions: admin view/revoke any (`?manager_id`); password-change / 2FA disable / reset revoke other sessions.
- Audit viewer: filters (actor/entity/action/date range), localizable `summary_key`+params, CSV export under `export`.
- `docs/ops/security-checklist.md` (ASVS L2 skeleton, verification = Phase 5).

**Tests/gate:** whole suite green (`go test ./...`, incl. DB-backed against Postgres+Redis); Phase-2 tests unbroken. Gate legs appended to `scripts/gate-phase-3.sh` (created it) — item 5 legs PASS.

**Deviations:** none — all C1-A/C7 contracts implemented as frozen.

**Seams left:** (1) seed's `admin` row has null `role_id` and relies on the resolve fallback (works; D may set `role_id` explicitly when convenient). (2) `scripts/lint-audit-calls.sh` flags `internal/live` (Agent C) — pre-existing, outside my ownership, untouched by me.
