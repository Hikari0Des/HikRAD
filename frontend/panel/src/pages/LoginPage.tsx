import { useState, type FormEvent } from 'react'
import { Navigate, useLocation, useNavigate } from 'react-router-dom'

import { useT } from '@hikrad/shared'

import { ApiError, NetworkError } from '../api/client'
import { useAuth } from '../auth/AuthContext'
import { LanguageSwitcher } from '../components/LanguageSwitcher'

export function LoginPage() {
  const { manager, login } = useAuth()
  const t = useT()
  const navigate = useNavigate()
  const location = useLocation()

  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(false)

  if (manager) return <Navigate to="/" replace />

  const from = (location.state as { from?: string } | null)?.from ?? '/'

  async function onSubmit(e: FormEvent) {
    e.preventDefault()
    setError(null)
    setLoading(true)
    try {
      await login(username, password)
      navigate(from, { replace: true })
    } catch (err) {
      if (err instanceof NetworkError) {
        // Backend down / unreachable — friendly, localized (task edge case).
        setError(t('login.error.network'))
      } else if (
        err instanceof ApiError &&
        (err.status === 400 || err.status === 401 || err.status === 422)
      ) {
        setError(t('login.error.invalid_credentials'))
      } else {
        setError(t('login.error.generic'))
      }
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="flex min-h-screen flex-col items-center justify-center gap-6 p-4">
      <div className="w-full max-w-sm rounded-lg bg-surface-raised p-6 shadow">
        <div className="mb-6 text-center">
          <div className="text-2xl font-bold text-brand">{t('common.productName')}</div>
          <h1 className="mt-1 text-sm text-ink-muted">
            {t('login.title')} — {t('login.subtitle')}
          </h1>
        </div>
        <form onSubmit={onSubmit} className="space-y-4">
          <div>
            <label htmlFor="username" className="mb-1 block text-sm font-medium">
              {t('login.username')}
            </label>
            <input
              id="username"
              name="username"
              type="text"
              autoComplete="username"
              autoCapitalize="none"
              required
              dir="ltr"
              value={username}
              onChange={(e) => setUsername(e.target.value)}
              className="w-full rounded-md border border-surface-sunken bg-surface px-3 py-2 text-sm focus:border-brand focus:outline-none"
            />
          </div>
          <div>
            <label htmlFor="password" className="mb-1 block text-sm font-medium">
              {t('login.password')}
            </label>
            <input
              id="password"
              name="password"
              type="password"
              autoComplete="current-password"
              required
              dir="ltr"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              className="w-full rounded-md border border-surface-sunken bg-surface px-3 py-2 text-sm focus:border-brand focus:outline-none"
            />
          </div>
          {error && (
            <p role="alert" className="rounded-md bg-danger/10 px-3 py-2 text-sm text-danger">
              {error}
            </p>
          )}
          <button
            type="submit"
            disabled={loading}
            className="w-full rounded-md bg-brand px-4 py-2 text-sm font-medium text-ink-inverse hover:bg-brand-strong disabled:opacity-60"
          >
            {loading ? t('login.submitting') : t('login.submit')}
          </button>
        </form>
      </div>
      <LanguageSwitcher />
    </div>
  )
}
