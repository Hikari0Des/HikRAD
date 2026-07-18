import { useState, type FormEvent } from 'react'

import { useT } from '@hikrad/shared'

import { ApiError } from '../api/client'
import { getMe, updateMe } from '../api/me'
import { Field, TextInput } from '../components/form'
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
        <Field label={t('portal.settings.nameLabel')} htmlFor="settings-name">
          <TextInput
            id="settings-name"
            type="text"
            value={name}
            onChange={(e) => setName(e.target.value)}
          />
        </Field>

        <Field label={t('portal.settings.phoneLabel')} htmlFor="settings-phone">
          <TextInput
            id="settings-phone"
            type="tel"
            dir="ltr"
            value={phone}
            onChange={(e) => setPhone(e.target.value)}
            placeholder={me.data ? undefined : t('common.loading')}
          />
        </Field>

        <Field label={t('portal.settings.emailLabel')} htmlFor="settings-email">
          <TextInput
            id="settings-email"
            type="email"
            dir="ltr"
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            placeholder={me.data ? undefined : t('common.loading')}
          />
        </Field>

        <hr className="border-surface-sunken" />

        <p role="alert" className="rounded-md bg-warning/10 p-3 text-xs text-ink">
          {t('portal.settings.passwordWarning')}
        </p>

        <Field label={t('portal.settings.newPasswordLabel')} htmlFor="settings-new-password">
          <TextInput
            id="settings-new-password"
            type="password"
            dir="ltr"
            autoComplete="new-password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
          />
        </Field>

        <Field
          label={t('portal.settings.confirmPasswordLabel')}
          htmlFor="settings-confirm-password"
        >
          <TextInput
            id="settings-confirm-password"
            type="password"
            dir="ltr"
            autoComplete="new-password"
            value={confirmPassword}
            onChange={(e) => setConfirmPassword(e.target.value)}
          />
        </Field>

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
