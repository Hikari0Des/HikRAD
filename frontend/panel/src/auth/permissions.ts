/**
 * Client-side permission model (contract C2). The login response carries only
 * the manager's role string (the frozen C7 shape), and C2 fixes the phase's
 * role→permission sets in code (backend `internal/auth/permissions.go`). We
 * mirror that same frozen matrix here so the UI can hide/disable actions the
 * manager lacks — and, per C2, every call site checks a *permission string*,
 * never a role name, so Phase 3's DB-backed matrix swaps in without touching
 * component code.
 *
 * The panel is a convenience layer only: the server re-checks every permission
 * (a hidden button is not a security boundary).
 */

// Action permissions granted independently of module verbs (C2).
export const PERM_DISCONNECT = 'disconnect'
export const PERM_EXPORT = 'export'
export const PERM_RENEW = 'renew'
export const PERM_TOPUP = 'topup'

const ROLE_ADMIN = 'admin'

// Non-admin sets, byte-for-byte the backend's rolePermissions map. Admin is
// intentionally absent — it is allow-all (see hasPermission).
const ROLE_PERMISSIONS: Record<string, ReadonlySet<string>> = {
  operator: new Set([
    'subscribers.view',
    'subscribers.create',
    'subscribers.edit',
    'profiles.view',
    'nas.view',
    'pools.view',
    'live.view',
    'sessions.view',
    'reports.view',
    'audit.view',
    PERM_RENEW,
    PERM_DISCONNECT,
    PERM_TOPUP,
    PERM_EXPORT,
  ]),
  agent: new Set(['subscribers.view', 'reports.view', PERM_RENEW]),
}

/** Whether a role grants a permission string. Admin is unconditionally allowed. */
export function hasPermission(role: string | undefined, permission: string): boolean {
  if (!role) return false
  if (role === ROLE_ADMIN) return true
  return ROLE_PERMISSIONS[role]?.has(permission) ?? false
}
