/**
 * Client-side permission model (contract C2/C7). Phase 2 mirrored a hardcoded
 * role→permission matrix; Phase 3 makes roles DB-backed and editable, so the
 * authoritative effective set is the `perms` claim the backend embeds in every
 * access token (see `internal/auth/tokens.go`). We decode that claim and gate
 * the UI against *permission strings* — never role names (C2). Admin carries the
 * wildcard `*` (allow-all).
 *
 * The panel is a convenience layer only: the server re-checks every permission,
 * so a hidden button is not a security boundary. If a token cannot be decoded
 * (e.g. a stale Phase-2 session) we fall back to the frozen builtin matrix keyed
 * on the manager's role string so the UI never silently opens up or locks out.
 */

// Module verb permissions used to gate whole screens/actions.
export const PERM_SUBSCRIBERS_VIEW = 'subscribers.view'
export const PERM_PROFILES_VIEW = 'profiles.view'
export const PERM_NAS_VIEW = 'nas.view'
export const PERM_POOLS_VIEW = 'pools.view'
export const PERM_LIVE_VIEW = 'live.view'
export const PERM_REPORTS_VIEW = 'reports.view'
export const PERM_AUDIT_VIEW = 'audit.view'
export const PERM_MANAGERS_VIEW = 'managers.view'
export const PERM_MANAGERS_CREATE = 'managers.create'
export const PERM_MANAGERS_EDIT = 'managers.edit'
export const PERM_MANAGERS_DELETE = 'managers.delete'
export const PERM_VOUCHERS_VIEW = 'vouchers.view'
export const PERM_VOUCHERS_CREATE = 'vouchers.create'
export const PERM_MONITORING_VIEW = 'monitoring.view'
export const PERM_MONITORING_EDIT = 'monitoring.edit'
export const PERM_SETTINGS_VIEW = 'settings.view'
export const PERM_SETTINGS_EDIT = 'settings.edit'
export const PERM_LICENSE_MANAGE = 'license.manage'
export const PERM_BACKUPS_VIEW = 'backups.view'
/** v2-2 (FR-79.2/77.1): generalizes card_payments.verify to every method. */
export const PERM_PAYMENT_TICKETS_VERIFY = 'payment_tickets.verify'
export const PERM_PAYMENT_PROVIDERS_MANAGE = 'payment_providers.manage'
export const PERM_SUBSCRIBERS_CREATE = 'subscribers.create'
export const PERM_NAS_EDIT = 'nas.edit'
/** v2 phase 7 (FR-87.1): one-click panel update, admin-only by default. */
export const PERM_SYSTEM_UPDATE = 'system.update'

// Bare action permissions granted independently of module verbs (C2/C7).
export const PERM_DISCONNECT = 'disconnect'
export const PERM_EXPORT = 'export'
export const PERM_RENEW = 'renew'
export const PERM_TOPUP = 'topup'
export const PERM_REFUND = 'refund'
/** v2 phase 4 (FR-68.3): rate creation is admin-only, an append-only audit trail. */
export const PERM_CURRENCY_RATES_MANAGE = 'currency_rates.manage'
/** v2 phase 9 (FR-73/74): overheads and reseller wholesale pricing are admin-only. */
export const PERM_OVERHEADS_MANAGE = 'overheads.manage'
export const PERM_RESELLER_PRICES_MANAGE = 'reseller_prices.manage'

const WILDCARD = '*'
const ROLE_ADMIN = 'admin'

// Fallback matrix — byte-for-byte the backend's builtin rolePermissions map,
// used only when a token has no decodable `perms` claim. Admin is allow-all and
// intentionally absent (see hasPermission).
const ROLE_PERMISSIONS: Record<string, ReadonlySet<string>> = {
  operator: new Set([
    PERM_SUBSCRIBERS_VIEW,
    'subscribers.create',
    'subscribers.edit',
    PERM_PROFILES_VIEW,
    PERM_NAS_VIEW,
    PERM_POOLS_VIEW,
    PERM_LIVE_VIEW,
    'sessions.view',
    PERM_REPORTS_VIEW,
    PERM_AUDIT_VIEW,
    PERM_RENEW,
    PERM_DISCONNECT,
    PERM_TOPUP,
    PERM_EXPORT,
  ]),
  agent: new Set([PERM_SUBSCRIBERS_VIEW, PERM_REPORTS_VIEW, PERM_RENEW]),
}

/** Base64url-decode a JWT payload segment (browser-safe, no Buffer). */
function decodeBase64Url(segment: string): string | null {
  try {
    const b64 = segment.replace(/-/g, '+').replace(/_/g, '/')
    const padded = b64 + '='.repeat((4 - (b64.length % 4)) % 4)
    return atob(padded)
  } catch {
    return null
  }
}

/**
 * Read the effective permission set from an access token's `perms` claim.
 * Returns null when the token is absent or unparseable so callers can fall back
 * to the role matrix.
 */
export function decodeTokenPerms(token: string | null | undefined): ReadonlySet<string> | null {
  if (!token) return null
  const parts = token.split('.')
  if (parts.length !== 3) return null
  const json = decodeBase64Url(parts[1])
  if (!json) return null
  try {
    const claims = JSON.parse(json) as { perms?: unknown }
    if (!Array.isArray(claims.perms)) return null
    return new Set(claims.perms.filter((p): p is string => typeof p === 'string'))
  } catch {
    return null
  }
}

/**
 * Whether a decoded permission set (or role fallback) grants a permission
 * string. The wildcard `*` (admin) grants everything.
 */
export function permsHave(perms: ReadonlySet<string>, permission: string): boolean {
  return perms.has(WILDCARD) || perms.has(permission)
}

/** Role-matrix fallback (Phase-2 tokens with no `perms` claim). */
export function hasPermission(role: string | undefined, permission: string): boolean {
  if (!role) return false
  if (role === ROLE_ADMIN) return true
  return ROLE_PERMISSIONS[role]?.has(permission) ?? false
}
