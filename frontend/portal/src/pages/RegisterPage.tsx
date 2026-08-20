import { useEffect, useState, type FormEvent } from 'react'
import { Link, useNavigate } from 'react-router-dom'

import { useLocale, useT } from '@hikrad/shared'

import { registerRequest, registerVerify } from '../api/guestreg'
import { useAuth } from '../auth/AuthContext'
import { brandInitial, useBranding } from '../branding'
import { PoweredByFooter } from '../shell/PoweredByFooter'

/** Self-service guest registration (FR-169): visitor connects on the
 * hotspot, taps Register, enters name + mobile, receives a passcode by
 * SMS/WhatsApp, enters it, gets a real named account. mac is read once from
 * the URL exactly like the auto-login captive hand-off (FR-168) — the
 * hotspot redirect's own $(mac) variable, a hint the server corroborates,
 * never trusts on its own. */
export function RegisterPage() {
  const t = useT()
  const { locale } = useLocale()
  const navigate = useNavigate()
  const branding = useBranding()
  const { adoptSession, subscriber } = useAuth()

  const [mac, setMac] = useState<string | undefined>(undefined)
  useEffect(() => {
    setMac(new URLSearchParams(window.location.search).get('mac') ?? undefined)
  }, [])
  useEffect(() => {
    if (subscriber) navigate('/home', { replace: true })
  }, [subscriber, navigate])

  const [name, setName] = useState('')
  const [phone, setPhone] = useState('')
  const [code, setCode] = useState('')
  const [sent, setSent] = useState(false)
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)

  async function onRequest(event: FormEvent) {
    event.preventDefault()
    if (!name || !phone) return
    setSubmitting(true)
    setError(null)
    try {
      const outcome = await registerRequest(name, phone, mac, locale)
      switch (outcome.kind) {
        case 'sent':
          setSent(true)
          break
        case 'cooldown':
          setError(t('portal.register.error.cooldown'))
          break
        case 'daily_cap_reached':
          setError(t('portal.register.error.dailyCap'))
          break
        case 'not_enabled':
        case 'not_licensed':
        case 'channel_not_configured':
          setError(t('portal.register.error.unavailable'))
          break
      }
    } catch {
      setError(t('portal.register.error.generic'))
    } finally {
      setSubmitting(false)
    }
  }

  async function onVerify(event: FormEvent) {
    event.preventDefault()
    if (!code) return
    setSubmitting(true)
    setError(null)
    try {
      const res = await registerVerify(phone, code, mac)
      adoptSession(res)
      navigate('/home')
    } catch {
      setError(t('portal.register.error.invalidCode'))
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
        <h1 className="text-xl font-semibold">{t('portal.register.title')}</h1>
        <p className="text-sm text-ink-muted">{t('portal.register.subtitle')}</p>
      </div>

      <form
        onSubmit={sent ? onVerify : onRequest}
        className="flex flex-col gap-4 rounded-xl bg-surface-raised p-6 shadow-sm"
      >
        <label className="flex flex-col gap-1 text-sm">
          {t('portal.register.nameLabel')}
          <input
            value={name}
            disabled={sent}
            onChange={(e) => setName(e.target.value)}
            className="rounded-md border border-border bg-surface px-3 py-2 text-base disabled:opacity-60"
          />
        </label>

        <label className="flex flex-col gap-1 text-sm">
          {t('portal.register.phoneLabel')}
          <input
            type="tel"
            dir="ltr"
            value={phone}
            disabled={sent}
            onChange={(e) => setPhone(e.target.value)}
            className="rounded-md border border-border bg-surface px-3 py-2 text-base disabled:opacity-60"
          />
        </label>

        {sent ? (
          <label className="flex flex-col gap-1 text-sm">
            {t('portal.register.codeLabel')}
            <input
              inputMode="numeric"
              dir="ltr"
              value={code}
              onChange={(e) => setCode(e.target.value)}
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
          {sent ? t('portal.register.verifySubmit') : t('portal.register.requestSubmit')}
        </button>
      </form>

      <p className="text-center text-sm">
        <Link to="/" className="font-semibold text-brand underline">
          {t('portal.register.backToLogin')}
        </Link>
      </p>
      <PoweredByFooter />
    </div>
  )
}
