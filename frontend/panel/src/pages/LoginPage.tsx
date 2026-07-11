import { useState, type FormEvent } from 'react'
import { Navigate, useLocation, useNavigate } from 'react-router-dom'

import { useT } from '@hikrad/shared'

import { ApiError, NetworkError } from '../api/client'
import { useAuth } from '../auth/AuthContext'
import { LanguageSwitcher } from '../components/LanguageSwitcher'
import { TotpEnroll } from './security/TotpEnroll'

type Step = 'credentials' | 'totp' | 'enroll'

export function LoginPage() {
  const { manager, login } = useAuth()
  const t = useT()
  const navigate = useNavigate()
  const location = useLocation()

  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [totpCode, setTotpCode] = useState('')
  const [step, setStep] = useState<Step>('credentials')
  const [enrollmentToken, setEnrollmentToken] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [info, setInfo] = useState<string | null>(null)
  const [loading, setLoading] = useState(false)

  if (manager) return <Navigate to="/" replace />

  const from = (location.state as { from?: string } | null)?.from ?? '/'

  async function attempt(code?: string) {
    setError(null)
    setLoading(true)
    try {
      const outcome = await login(username, password, code)
      switch (outcome.kind) {
        case 'session':
          navigate(from, { replace: true })
          break
        case 'totp_required':
          if (code) setError(t('login.error.invalid_totp'))
          setStep('totp')
          break
        case 'enroll':
          setEnrollmentToken(outcome.enrollmentToken)
          setStep('enroll')
          break
      }
    } catch (err) {
      if (err instanceof NetworkError) {
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

  function onCredentials(e: FormEvent) {
    e.preventDefault()
    void attempt()
  }

  function onTotp(e: FormEvent) {
    e.preventDefault()
    void attempt(totpCode.trim())
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

        {info && (
          <p className="mb-4 rounded-md bg-brand-soft px-3 py-2 text-sm text-brand-strong">
            {info}
          </p>
        )}

        {step === 'enroll' ? (
          <div className="space-y-4">
            <p className="text-sm text-ink-muted">{t('login.enrollRequired')}</p>
            <TotpEnroll
              enrollmentToken={enrollmentToken}
              onEnrolled={() => {
                setStep('totp')
                setInfo(t('login.enrollDone'))
                setPassword('')
              }}
            />
          </div>
        ) : step === 'totp' ? (
          <form onSubmit={onTotp} className="space-y-4">
            <p className="text-sm text-ink-muted">{t('login.totpPrompt')}</p>
            <div>
              <label htmlFor="totp" className="mb-1 block text-sm font-medium">
                {t('login.totpCode')}
              </label>
              <input
                id="totp"
                name="totp"
                inputMode="numeric"
                autoComplete="one-time-code"
                required
                dir="ltr"
                value={totpCode}
                onChange={(e) => setTotpCode(e.target.value)}
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
              {loading ? t('login.submitting') : t('login.verifyCode')}
            </button>
          </form>
        ) : (
          <form onSubmit={onCredentials} className="space-y-4">
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
        )}
      </div>
      <LanguageSwitcher />
    </div>
  )
}
