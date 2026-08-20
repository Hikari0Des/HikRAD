import { createContext, useContext, useEffect, useMemo, useState, type ReactNode } from 'react'
import { Navigate, useLocation } from 'react-router-dom'

import {
  login as apiLogin,
  logout as apiLogout,
  type LoginOutcome,
  type SessionScope,
  type Subscriber,
} from '../api/auth'
import { stepUp as apiStepUp, tryAutoLogin } from '../api/autologin'
import { UNAUTHORIZED_EVENT } from '../api/client'
import { tokenStore } from './tokenStore'

interface AuthContextValue {
  subscriber: Subscriber | null
  scope: SessionScope
  /** True only while the one-time silent auto-login probe (FR-167) is still
   * in flight on first load — callers show a neutral loading state, never a
   * flash of the login form that then disappears. */
  bootstrapping: boolean
  login: (username: string, password: string) => Promise<LoginOutcome>
  /** Sets the session directly from an already-completed login-shaped
   * response (auto-login, guest registration, OTP verify — every path that
   * ends in the same {access_token, subscriber, scope} shape). */
  adoptSession: (res: {
    access_token: string
    refresh_token?: string
    subscriber: Subscriber
    scope: SessionScope
  }) => void
  stepUp: (password: string) => Promise<'ok' | 'invalid_password'>
  logout: () => void
}

const AuthContext = createContext<AuthContextValue | null>(null)

/** mac/ip captive-portal hand-off hints (FR-168): read once from the URL a
 * hotspot redirect landed on, e.g. .../?mac=AA:BB:CC:DD:EE:FF&ip=10.0.0.5 —
 * RouterOS's own login-page variables, never trusted on their own (the
 * server validates them against the live-session table, same path as
 * plain auto-login). */
function captiveHints(): { mac?: string; ip?: string } {
  const params = new URLSearchParams(window.location.search)
  return { mac: params.get('mac') ?? undefined, ip: params.get('ip') ?? undefined }
}

export function AuthProvider({ children }: { children: ReactNode }) {
  const [subscriber, setSubscriber] = useState<Subscriber | null>(() => tokenStore.getSubscriber())
  const [scope, setScope] = useState<SessionScope>(() => tokenStore.getScope())
  const [bootstrapping, setBootstrapping] = useState(() => !tokenStore.getSubscriber())

  // A dead refresh chain (revoked/expired) fires this; drop the session so
  // <RequireAuth> redirects to the login screen.
  useEffect(() => {
    const onUnauthorized = () => {
      setSubscriber(null)
      setScope('full')
    }
    window.addEventListener(UNAUTHORIZED_EVENT, onUnauthorized)
    return () => window.removeEventListener(UNAUTHORIZED_EVENT, onUnauthorized)
  }, [])

  // FR-167's whole product moment: a subscriber opening the portal from a
  // device with a live session is signed in without typing anything. Only
  // ever attempted once, on first load, when nobody is already signed in —
  // never re-attempted on every unauthenticated render (that would spam the
  // endpoint and its audit trail for no reason).
  useEffect(() => {
    if (tokenStore.getSubscriber()) {
      setBootstrapping(false)
      return
    }
    let cancelled = false
    tryAutoLogin(captiveHints())
      .then((res) => {
        if (cancelled || !res) return
        tokenStore.setTokens(res.access_token, res.refresh_token)
        tokenStore.setSubscriber(res.subscriber)
        tokenStore.setScope(res.scope)
        setSubscriber(res.subscriber)
        setScope(res.scope)
      })
      .finally(() => {
        if (!cancelled) setBootstrapping(false)
      })
    return () => {
      cancelled = true
    }
  }, [])

  const value = useMemo<AuthContextValue>(
    () => ({
      subscriber,
      scope,
      bootstrapping,
      async login(username, password) {
        const outcome = await apiLogin(username, password)
        if (outcome.kind === 'session') {
          const { access_token, refresh_token, subscriber: sub, scope: sc } = outcome.response
          tokenStore.setTokens(access_token, refresh_token)
          tokenStore.setSubscriber(sub)
          tokenStore.setScope(sc)
          setSubscriber(sub)
          setScope(sc)
        }
        return outcome
      },
      adoptSession(res) {
        tokenStore.setTokens(res.access_token, res.refresh_token)
        tokenStore.setSubscriber(res.subscriber)
        tokenStore.setScope(res.scope)
        setSubscriber(res.subscriber)
        setScope(res.scope)
      },
      async stepUp(password) {
        const outcome = await apiStepUp(password)
        if (outcome.kind === 'ok') {
          tokenStore.setTokens(outcome.response.access_token, outcome.response.refresh_token)
          tokenStore.setScope(outcome.response.scope)
          setScope(outcome.response.scope)
          return 'ok'
        }
        return 'invalid_password'
      },
      logout() {
        void apiLogout().catch(() => {})
        tokenStore.clear()
        setSubscriber(null)
        setScope('full')
      },
    }),
    [subscriber, scope, bootstrapping],
  )

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>
}

export function useAuth(): AuthContextValue {
  const ctx = useContext(AuthContext)
  if (!ctx) throw new Error('useAuth must be used inside <AuthProvider>')
  return ctx
}

export function RequireAuth({ children }: { children: ReactNode }) {
  const { subscriber, bootstrapping } = useAuth()
  const location = useLocation()
  // Neutral (blank) while the silent auto-login probe is in flight — never a
  // flash of the login form immediately replaced by the home screen.
  if (bootstrapping) return null
  if (!subscriber) {
    return <Navigate to="/" replace state={{ from: location.pathname }} />
  }
  return children
}
