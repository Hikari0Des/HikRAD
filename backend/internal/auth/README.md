# internal/auth — Platform & Security

Owns manager authentication, the permission/scoping model every API module
adopts (`auth.Require` / `auth.ScopeFilter` / `auth.Audit`), TOTP 2FA, panel
sessions, per-manager IP allowlists, and the audit-log viewer. It also installs
the process-wide crypto default (`platform/crypto`).

## Permission model: Phase 2 → Phase 3 migration

**Phase 2** shipped *hardcoded* role→permission sets: `rolePermissions` in
[permissions.go](permissions.go) mapped a role name to a fixed set, and
`Manager.Can(perm)` looked the caller's role up in that map on every check.

**Phase 3** makes roles editable DB rows without changing a single call site.
The frozen contract is unchanged: **code checks permission strings**
(`<module>.<verb>` and bare action perms like `renew`), **never role names**.

What changed under the hood:

1. **Schema** (migrations `0210`–`0213`): `roles`, `role_permissions`
   (`permission` text, wildcard `*` = allow-all), `managers.role_id`,
   `manager_permission_overrides`, `manager_ip_allowlist`, plus TOTP pending
   secret + `manager_backup_codes`. The `0210` migration seeds the three builtin
   roles (Admin/Operator/Agent) with **exactly** the Phase-2 sets and backfills
   `managers.role_id` from the legacy `role` text, so the swap is
   behaviour-preserving.

2. **Resolution** (`resolvePermissions`): the effective set = the assigned
   role's permissions + per-manager overrides (grant adds, revoke removes). It
   runs **once, at login/refresh**, and the result is embedded in the access
   token (`perms` claim). `Manager.Can` reads that embedded set (wildcard `*`
   grants all) — no per-request DB hit. A role/override edit therefore takes
   effect **within one access-token lifetime** (≤ 5 min), the same propagation
   SLA as session revocation.

3. **Fallback**: a `Manager` built without a resolved set (unit tests, or a
   legacy row whose `role_id` is null) falls back to the in-memory
   `rolePermissions` map keyed on the legacy role text. This is why the Phase-2
   `permissions_test.go` cases (`&Manager{Role: "operator"}`) still pass
   unchanged.

The legacy `managers.role` text column is retained for display/back-compat and
kept in sync with `role_id` on writes; `role_id` is authoritative.

## Escalation guard

Role/override editing enforces a privilege-escalation guard: an editor cannot
grant a permission they do not themselves hold (`escalationCheck`). An editor
with `*` (admin) may grant anything.

## TOTP 2FA (FR-28.1)

`POST /api/v1/auth/totp/enroll` → otpauth URI + base32 key (stores a *pending*
sealed secret) → `…/verify` (activates, returns 10 one-time backup codes) →
`…/disable` (password + code). Admins reset a locked-out manager via
`POST /api/v1/managers/{id}/totp/reset`. Login gains a TOTP step when the
account is enrolled; when 2FA is *required* (global `security.require_2fa`
setting or the role's `require_2fa`) but not yet set up, login returns a limited
**enrolment grant** token accepted only by the enroll/verify endpoints.

## IP allowlist (FR-30)

Per-manager CIDR list, empty = unrestricted. Enforced at login and on every
request (embedded in the token as the `ips` claim). Client IP comes from the
first `X-Forwarded-For` hop set by Caddy — the only trusted ingress (NFR-4.4).

## Flat v1 (FR-27.4)

One role per manager; no manager-under-manager relations. The Phase-2 reseller
tree must be additive — see the master PRD.
