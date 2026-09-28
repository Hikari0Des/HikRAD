import { useState, type FormEvent } from 'react'

import { useT } from '@hikrad/shared'

import { useAuth } from '../auth/AuthContext'

/** FR-167: shown whenever the current session is the reduced (auto-login)
 * grant — view/renew/pay only. Offers the password step-up inline; the
 * server enforces the actual restriction regardless of whether this banner
 * is even shown (this is UX, not the security control). */
export function ReducedScopeBanner() {
  const t = useT()
  const { scope, stepUp } = useAuth()
  const [open, setOpen] = useState(false)
  const [password, setPassword] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)

  if (scope !== 'reduced') return null

  async function onSubmit(event: FormEvent) {
    event.preventDefault()
    if (!password) return
    setSubmitting(true)
    setError(null)
    const outcome = await stepUp(password)
    setSubmitting(false)
    if (outcome === 'ok') {
      setOpen(false)
      setPassword('')
    } else {
      setError(t('portal.stepUp.errorInvalid'))
    }
  }

  return (
    <div className="mx-4 mt-2 flex flex-col gap-2 rounded-xl border border-brand/30 bg-brand/5 p-3 text-xs">
      <div className="flex items-center justify-between gap-2">
        <span>{t('portal.stepUp.bannerText')}</span>
        {!open ? (
          <button
            type="button"
            onClick={() => setOpen(true)}
            className="shrink-0 font-semibold text-brand underline"
          >
            {t('portal.stepUp.verifyAction')}
          </button>
        ) : null}
      </div>
      {open ? (
        <form onSubmit={onSubmit} className="flex items-center gap-2">
          <input
            type="password"
            dir="ltr"
            autoComplete="current-password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            placeholder={t('portal.login.password')}
            className="min-w-0 flex-1 rounded-md border border-border bg-surface px-2 py-1"
          />
          <button
            type="submit"
            disabled={submitting}
            className="shrink-0 rounded-md bg-brand px-3 py-1 font-semibold text-ink-inverse disabled:opacity-60"
          >
            {t('portal.stepUp.submit')}
          </button>
        </form>
      ) : null}
      {error ? (
        <p role="alert" className="text-danger">
          {error}
        </p>
      ) : null}
    </div>
  )
}
