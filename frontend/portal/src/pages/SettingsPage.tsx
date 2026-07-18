import { useState, type FormEvent } from 'react'

import { useT } from '@hikrad/shared'

import { ApiError } from '../api/client'
import { getMe, updateMe } from '../api/me'
import { useAsync } from '../hooks/useAsync'

/** Account self-care (FR-44): phone + password, subscriber-safe fields only —
 * never profile, expiry, MAC, or status. Password change re-encrypts
 * server-side and invalidates the cached RADIUS policy, so the PPPoE
 * credential changes too — the UI must warn about that up front. */
export function SettingsPage() {
  const t = useT()
  const me = useAsync(getMe, [])
  const [name, setName] = useState('')
  const [phone, setPhone] = useState('')
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [confirmPassword, setConfirmPassword] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [saved, setSaved] = useState(false)

  async function onSubmit(e: FormEvent) {
    e.preventDefault()
    setError(null)
    setSaved(false)
    if (password && password !== confirmPassword) {
      setError(t('portal.settings.error.passwordMismatch'))
      return
    }
    setSubmitting(true)
    try {
      await updateMe({
        name: name || undefined,
        phone: phone || undefined,
        email: email || undefined,
        password: password || undefined,
      })
      setSaved(true)
      setPassword('')
      setConfirmPassword('')
      me.reload()
    } catch (err) {
      if (err instanceof ApiError && err.fieldErrors.length > 0) {
        setError(err.fieldErrors.map((f) => f.message).join(' '))
      } else {
        setError(t('portal.settings.error.generic'))
      }
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <section className="flex flex-col gap-4">
      <h1 className="text-lg font-semibold">{t('portal.settings.title')}</h1>

      <form
        onSubmit={onSubmit}
        className="flex flex-col gap-4 rounded-xl bg-surface-raised p-4 shadow-sm"
      >
        <label className="flex flex-col gap-1 text-sm">
          {t('portal.settings.nameLabel')}
          <input
            type="text"
            value={name}
            onChange={(e) => setName(e.target.value)}
            className="rounded-md border border-surface-sunken bg-surface px-3 py-2 text-base"
          />
        </label>

        <label className="flex flex-col gap-1 text-sm">
          {t('portal.settings.phoneLabel')}
          <input
            type="tel"
            dir="ltr"
            value={phone}
            onChange={(e) => setPhone(e.target.value)}
            placeholder={me.data ? undefined : t('common.loading')}
            className="rounded-md border border-surface-sunken bg-surface px-3 py-2 text-base"
          />
        </label>

        <label className="flex flex-col gap-1 text-sm">
          {t('portal.settings.emailLabel')}
          <input
            type="email"
            dir="ltr"
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            placeholder={me.data ? undefined : t('common.loading')}
            className="rounded-md border border-surface-sunken bg-surface px-3 py-2 text-base"
          />
        </label>

        <hr className="border-surface-sunken" />

        <p role="alert" className="rounded-md bg-warning/10 p-3 text-xs text-ink">
          {t('portal.settings.passwordWarning')}
        </p>

        <label className="flex flex-col gap-1 text-sm">
          {t('portal.settings.newPasswordLabel')}
          <input
            type="password"
            dir="ltr"
            autoComplete="new-password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            className="rounded-md border border-surface-sunken bg-surface px-3 py-2 text-base"
          />
        </label>

        <label className="flex flex-col gap-1 text-sm">
          {t('portal.settings.confirmPasswordLabel')}
          <input
            type="password"
            dir="ltr"
            autoComplete="new-password"
            value={confirmPassword}
            onChange={(e) => setConfirmPassword(e.target.value)}
            className="rounded-md border border-surface-sunken bg-surface px-3 py-2 text-base"
          />
        </label>

        {error ? (
          <p role="alert" className="text-sm text-danger">
            {error}
          </p>
        ) : null}
        {saved ? (
          <p role="status" className="text-sm text-ok">
            {t('portal.settings.saved')}
          </p>
        ) : null}

        <button
          type="submit"
          disabled={submitting}
          className="rounded-md bg-brand py-2 font-semibold text-ink-inverse disabled:opacity-60"
        >
          {submitting ? t('portal.settings.submitting') : t('portal.settings.submit')}
        </button>
      </form>
    </section>
  )
}
