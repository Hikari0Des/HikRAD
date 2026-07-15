import { createContext, useContext, useEffect, useMemo, useState, type ReactNode } from 'react'
import { Navigate, useLocation } from 'react-router-dom'

import {
  login as apiLogin,
  logout as apiLogout,
  type LoginOutcome,
  type Subscriber,
} from '../api/auth'
import { UNAUTHORIZED_EVENT } from '../api/client'
import { tokenStore } from './tokenStore'

interface AuthContextValue {
  subscriber: Subscriber | null
  login: (username: string, password: string) => Promise<LoginOutcome>
  logout: () => void
}

const AuthContext = createContext<AuthContextValue | null>(null)

export function AuthProvider({ children }: { children: ReactNode }) {
  const [subscriber, setSubscriber] = useState<Subscriber | null>(() => tokenStore.getSubscriber())

  // A dead refresh chain (revoked/expired) fires this; drop the session so
  // <RequireAuth> redirects to the login screen.
  useEffect(() => {
    const onUnauthorized = () => setSubscriber(null)
    window.addEventListener(UNAUTHORIZED_EVENT, onUnauthorized)
    return () => window.removeEventListener(UNAUTHORIZED_EVENT, onUnauthorized)
  }, [])

  const value = useMemo<AuthContextValue>(
    () => ({
      subscriber,
      async login(username, password) {
        const outcome = await apiLogin(username, password)
        if (outcome.kind === 'session') {
          const { access_token, refresh_token, subscriber: sub } = outcome.response
          tokenStore.setTokens(access_token, refresh_token)
          tokenStore.setSubscriber(sub)
          setSubscriber(sub)
        }
        return outcome
      },
      logout() {
        void apiLogout().catch(() => {})
        tokenStore.clear()
        setSubscriber(null)
      },
    }),
    [subscriber],
  )

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>
}

export function useAuth(): AuthContextValue {
  const ctx = useContext(AuthContext)
  if (!ctx) throw new Error('useAuth must be used inside <AuthProvider>')
  return ctx
}

export function RequireAuth({ children }: { children: ReactNode }) {
  const { subscriber } = useAuth()
  const location = useLocation()
  if (!subscriber) {
    return <Navigate to="/" replace state={{ from: location.pathname }} />
  }
  return children
}
