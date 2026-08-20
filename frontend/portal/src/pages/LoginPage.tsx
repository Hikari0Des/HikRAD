import { useEffect, useState, type FormEvent } from 'react'
import { Link, useNavigate } from 'react-router-dom'

import { useT } from '@hikrad/shared'

import { otpRequest, otpVerify } from '../api/guestreg'
import { useAuth } from '../auth/AuthContext'
import { brandInitial, useBranding } from '../branding'
import { LanguageSwitcher } from '../components/LanguageSwitcher'
import { PoweredByFooter } from '../shell/PoweredByFooter'

/** Branded subscriber login (FR-41.1, FR-43): same credential as PPPoE,
 * rate-limited server-side (NFR-4.6), friendly errors for the disabled/
 * rate-limited cases rather than a raw API message. Also the OTP-login
 * entry point (FR-172) for passwordless hotspot accounts, and the doorway
 * to self-service registration (FR-169, /register). FR-167's auto-login
 * probe runs silently in AuthProvider before this page is ever shown a
 * device with a live session; this form is only reached when that probe
 * declined or hasn't run yet. */
export function LoginPage() {
  const t = useT()
  const navigate = useNavigate()
  const { login, adoptSession, subscriber, bootstrapping } = useAuth()
  const branding = useBranding()
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const [otpMode, setOtpMode] = useState(false)
  const [otpCode, setOtpCode] = useState('')
  const [otpSent, setOtpSent] = useState(false)

  // Auto-login (FR-167) may resolve AFTER this page has already rendered
  // (the probe runs in the background) — jump to home the moment it lands,
  // rather than waiting for a full remount.
  useEffect(() => {
    if (subscriber) navigate('/home', { replace: true })
  }, [subscriber, navigate])

  if (bootstrapping) return null

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

  // FR-172: passwordless hotspot accounts have no other portal credential.
  async function onOtpRequest(event: FormEvent) {
    event.preventDefault()
    if (!username) return
    setSubmitting(true)
    setError(null)
    try {
      const outcome = await otpRequest(username)
      if (outcome.kind === 'sent') {
        setOtpSent(true)
      } else {
        setError(t('portal.login.otp.errorRequest'))
      }
    } catch {
      setError(t('portal.login.otp.errorRequest'))
    } finally {
      setSubmitting(false)
    }
  }

  async function onOtpVerify(event: FormEvent) {
    event.preventDefault()
    if (!otpCode) return
    setSubmitting(true)
    setError(null)
    try {
      const res = await otpVerify(username, otpCode)
      adoptSession(res)
      navigate('/home')
    } catch {
      setError(t('portal.login.otp.errorVerify'))
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

      {!otpMode ? (
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
              className="rounded-md border border-border bg-surface px-3 py-2 text-base"
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
              className="rounded-md border border-border bg-surface px-3 py-2 text-base"
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

          <button
            type="button"
            onClick={() => {
              setOtpMode(true)
              setOtpSent(false)
              setError(null)
            }}
            className="text-xs font-medium text-brand underline"
          >
            {t('portal.login.otp.switchToOtp')}
          </button>
        </form>
      ) : (
        <form
          onSubmit={otpSent ? onOtpVerify : onOtpRequest}
          className="flex flex-col gap-4 rounded-xl bg-surface-raised p-6 shadow-sm"
        >
          <h2 className="text-base font-semibold">{t('portal.login.otp.title')}</h2>
          <p className="text-xs text-ink-muted">{t('portal.login.otp.subtitle')}</p>

          <label className="flex flex-col gap-1 text-sm">
            {t('portal.login.username')}
            <input
              name="otp-username"
              autoComplete="username"
              dir="ltr"
              value={username}
              disabled={otpSent}
              onChange={(e) => setUsername(e.target.value)}
              className="rounded-md border border-border bg-surface px-3 py-2 text-base disabled:opacity-60"
            />
          </label>

          {otpSent ? (
            <label className="flex flex-col gap-1 text-sm">
              {t('portal.login.otp.codeLabel')}
              <input
                name="otp-code"
                inputMode="numeric"
                dir="ltr"
                value={otpCode}
                onChange={(e) => setOtpCode(e.target.value)}
                className="rounded-md border border-border bg-surface px-3 py-2 text-base"
              />
            </label>
          ) : null}

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
            {otpSent
              ? t('portal.login.otp.verifySubmit')
              : submitting
                ? t('portal.login.otp.requesting')
                : t('portal.login.otp.requestSubmit')}
          </button>

          <button
            type="button"
            onClick={() => {
              setOtpMode(false)
              setOtpSent(false)
              setError(null)
            }}
            className="text-xs font-medium text-brand underline"
          >
            {t('portal.login.otp.switchToPassword')}
          </button>
        </form>
      )}

      <p className="text-center text-sm">
        {t('portal.login.registerPrompt')}{' '}
        <Link to="/register" className="font-semibold text-brand underline">
          {t('portal.login.registerAction')}
        </Link>
      </p>

      <div className="flex justify-center">
        <LanguageSwitcher />
      </div>
      <PoweredByFooter />
    </div>
  )
}
