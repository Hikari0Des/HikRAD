import { createContext, useContext, useEffect, useMemo, useState, type ReactNode } from 'react'
import { Navigate, useLocation } from 'react-router-dom'

import { login as apiLogin, type Manager } from '../api/auth'
import { UNAUTHORIZED_EVENT } from '../api/client'
import { tokenStore } from './tokenStore'

interface AuthContextValue {
  manager: Manager | null
  login: (username: string, password: string) => Promise<void>
  logout: () => void
}

const AuthContext = createContext<AuthContextValue | null>(null)

export function AuthProvider({ children }: { children: ReactNode }) {
  const [manager, setManager] = useState<Manager | null>(() => tokenStore.getManager())

  // The API client clears tokens and fires this on an unrecoverable 401;
  // dropping the manager makes <RequireAuth> redirect to /login.
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
        tokenStore.clear()
        setManager(null)
      },
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

export function RequireAuth({ children }: { children: ReactNode }) {
  const { manager } = useAuth()
  const location = useLocation()
  if (!manager) {
    return <Navigate to="/login" replace state={{ from: location.pathname }} />
  }
  return children
}
