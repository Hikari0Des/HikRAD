import { createContext, useContext, useEffect, useMemo, useState, type ReactNode } from 'react'
import { Navigate, useLocation } from 'react-router-dom'

import { login as apiLogin, logout as apiLogout, type Manager } from '../api/auth'
import { UNAUTHORIZED_EVENT } from '../api/client'
import { hasPermission } from './permissions'
import { tokenStore } from './tokenStore'

interface AuthContextValue {
  manager: Manager | null
  login: (username: string, password: string) => Promise<void>
  logout: () => void
  /** Whether the signed-in manager holds a permission string (contract C2). */
  can: (permission: string) => boolean
}

const AuthContext = createContext<AuthContextValue | null>(null)

export function AuthProvider({ children }: { children: ReactNode }) {
  const [manager, setManager] = useState<Manager | null>(() => tokenStore.getManager())

  // The API client clears tokens and fires this on an unrecoverable 401 (a
  // revoked/expired refresh chain — FR-29 forced logout); dropping the manager
  // makes <RequireAuth> redirect to /login.
  useEffect(() => {
    const onUnauthorized = () => setManager(null)
    window.addEventListener(UNAUTHORIZED_EVENT, onUnauthorized)
    return () => window.removeEventListener(UNAUTHORIZED_EVENT, onUnauthorized)
  }, [])

  const value = useMemo<AuthContextValue>(
    () => ({
      manager,
      async login(username, password) {
        const res = await apiLogin(username, password)
        tokenStore.setTokens(res.access_token, res.refresh_token)
        tokenStore.setManager(res.manager)
        setManager(res.manager)
      },
      logout() {
        // Best-effort server-side revocation, then drop the local session
        // regardless of the result.
        void apiLogout().catch(() => {})
        tokenStore.clear()
        setManager(null)
      },
      can: (permission) => hasPermission(manager?.role, permission),
    }),
    [manager],
  )

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>
}

export function useAuth(): AuthContextValue {
  const ctx = useContext(AuthContext)
  if (!ctx) throw new Error('useAuth must be used inside <AuthProvider>')
  return ctx
}

/** Convenience hook for the common `can(permission)` gate. */
export function useCan(permission: string): boolean {
  return useAuth().can(permission)
}

export function RequireAuth({ children }: { children: ReactNode }) {
  const { manager } = useAuth()
  const location = useLocation()
  if (!manager) {
    return <Navigate to="/login" replace state={{ from: location.pathname }} />
  }
  return children
}
