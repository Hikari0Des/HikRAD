import { useState, type FormEvent } from 'react'
import { useNavigate } from 'react-router-dom'

import { useT } from '@hikrad/shared'

import { useAuth } from '../auth/AuthContext'
import { brandInitial, useBranding } from '../branding'
import { LanguageSwitcher } from '../components/LanguageSwitcher'

/** Branded subscriber login (FR-41.1, FR-43): same credential as PPPoE,
 * rate-limited server-side (NFR-4.6), friendly errors for the disabled/
 * rate-limited cases rather than a raw API message. */
export function LoginPage() {
  const t = useT()
  const navigate = useNavigate()
  const { login } = useAuth()
  const branding = useBranding()
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)

  async function onSubmit(event: FormEvent) {
    event.preventDefault()
    if (!username || !password) return
    setSubmitting(true)
    setError(null)
    try {
      const outcome = await login(username, password)
      switch (outcome.kind) {
        case 'session':
          navigate('/home')
          break
        case 'invalid_credentials':
          setError(t('portal.login.errorInvalid'))
          break
        case 'rate_limited':
          setError(t('portal.login.errorRateLimited'))
          break
        case 'disabled':
          setError(t('portal.login.errorDisabled'))
          break
      }
    } catch {
      setError(t('portal.login.errorNetwork'))
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <div className="mx-auto flex min-h-screen w-full max-w-md flex-col justify-center gap-6 px-4 py-8">
      <div className="flex flex-col items-center gap-2 text-center">
        {branding.logo_url ? (
          <img
            src={branding.logo_url}
            alt={branding.name}
            className="h-14 w-14 rounded-xl object-contain"
          />
        ) : (
          <span
            aria-hidden="true"
            className="flex h-14 w-14 items-center justify-center rounded-xl bg-brand text-2xl font-bold text-ink-inverse"
          >
            {brandInitial(branding.name)}
          </span>
        )}
        <h1 className="text-xl font-semibold">{branding.name}</h1>
        <p className="text-sm text-ink-muted">{t('portal.login.subtitle')}</p>
      </div>

      <form
        onSubmit={onSubmit}
        className="flex flex-col gap-4 rounded-xl bg-surface-raised p-6 shadow-sm"
      >
        <h2 className="text-base font-semibold">{t('portal.login.title')}</h2>

        <label className="flex flex-col gap-1 text-sm">
          {t('portal.login.username')}
          <input
            name="username"
            autoComplete="username"
            dir="ltr"
            value={username}
            onChange={(e) => setUsername(e.target.value)}
            className="rounded-md border border-surface-sunken bg-surface px-3 py-2 text-base"
          />
        </label>

        <label className="flex flex-col gap-1 text-sm">
          {t('portal.login.password')}
          <input
            name="password"
            type="password"
            autoComplete="current-password"
            dir="ltr"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            className="rounded-md border border-surface-sunken bg-surface px-3 py-2 text-base"
          />
        </label>

        {error ? (
          <p role="alert" className="text-sm text-danger">
            {error}
          </p>
        ) : null}

        <button
          type="submit"
          disabled={submitting}
          className="rounded-md bg-brand py-2 font-semibold text-ink-inverse transition-colors hover:bg-brand-strong disabled:opacity-60"
        >
          {submitting ? t('portal.login.submitting') : t('portal.login.submit')}
        </button>

        <p className="text-xs text-ink-muted">{t('portal.login.note')}</p>
      </form>

      <div className="flex justify-center">
        <LanguageSwitcher />
      </div>
    </div>
  )
}
