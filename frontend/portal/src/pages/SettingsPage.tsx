import { useState, type FormEvent } from 'react'

import { useT } from '@hikrad/shared'

import { ApiError } from '../api/client'
import { getMe, updateMe } from '../api/me'
import {
  getNotificationPrefs,
  updateNotificationPrefs,
  type NotificationPrefs,
} from '../api/notificationPrefs'
import { useAuth } from '../auth/AuthContext'
import { Field, Switch, TextInput } from '../components/form'
import { useAsync } from '../hooks/useAsync'

/** Account self-care (FR-44): phone + password, subscriber-safe fields only —
 * never profile, expiry, MAC, or status. Password change re-encrypts
 * server-side and invalidates the cached RADIUS policy, so the PPPoE
 * credential changes too — the UI must warn about that up front. Also
 * notification-channel preferences (FR-170). Every write on this page is
 * one of FR-167's four full-scope-only actions — a reduced (auto-login)
 * session sees a step-up prompt instead of the forms; the server enforces
 * this independently regardless of what this page renders. The gate and the
 * form are separate COMPONENTS (not a conditional return inside one) so
 * neither ever calls hooks conditionally. */
export function SettingsPage() {
  const { scope } = useAuth()
  return scope === 'reduced' ? <StepUpGate /> : <AccountForm />
}

function StepUpGate() {
  const t = useT()
  const { stepUp } = useAuth()
  const [password, setPassword] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)

  async function onSubmit(e: FormEvent) {
    e.preventDefault()
    if (!password) return
    setSubmitting(true)
    setError(null)
    const outcome = await stepUp(password)
    setSubmitting(false)
    if (outcome !== 'ok') setError(t('portal.stepUp.errorInvalid'))
  }

  return (
    <section className="flex flex-col gap-4">
      <h1 className="text-lg font-semibold">{t('portal.settings.title')}</h1>
      <form
        onSubmit={onSubmit}
        className="flex flex-col gap-3 rounded-xl bg-surface-raised p-6 shadow-sm"
      >
        <p className="text-sm text-ink-muted">{t('portal.stepUp.settingsGateText')}</p>
        <Field label={t('portal.login.password')} htmlFor="stepup-password">
          <TextInput
            id="stepup-password"
            type="password"
            dir="ltr"
            autoComplete="current-password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
          />
        </Field>
        {error ? (
          <p role="alert" className="text-sm text-danger">
            {error}
          </p>
        ) : null}
        <button
          type="submit"
          disabled={submitting}
          className="rounded-md bg-brand py-2 font-semibold text-ink-inverse disabled:opacity-60"
        >
          {t('portal.stepUp.submit')}
        </button>
      </form>
    </section>
  )
}

function AccountForm() {
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

        <hr className="border-border" />

        <p role="alert" className="rounded-md bg-warn/10 p-3 text-xs text-ink">
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

      <NotificationPrefsSection />
    </section>
  )
}

function NotificationPrefsSection() {
  const t = useT()
  const prefs = useAsync(getNotificationPrefs, [])
  const [saving, setSaving] = useState(false)
  const [saved, setSaved] = useState(false)

  async function onToggle(next: NotificationPrefs) {
    setSaving(true)
    setSaved(false)
    try {
      await updateNotificationPrefs(next)
      prefs.reload()
      setSaved(true)
    } finally {
      setSaving(false)
    }
  }

  if (prefs.loading || !prefs.data) return null

  return (
    <div className="flex flex-col gap-3 rounded-xl bg-surface-raised p-4 shadow-sm">
      <h2 className="text-sm font-semibold">{t('portal.settings.notifications.title')}</h2>
      <Switch
        label={t('portal.settings.notifications.sms')}
        checked={prefs.data.sms}
        disabled={saving}
        onChange={(e) => onToggle({ ...prefs.data!, sms: e.target.checked })}
      />
      <Switch
        label={t('portal.settings.notifications.email')}
        checked={prefs.data.email}
        disabled={saving}
        onChange={(e) => onToggle({ ...prefs.data!, email: e.target.checked })}
      />
      <Switch
        label={t('portal.settings.notifications.whatsapp')}
        checked={prefs.data.whatsapp}
        disabled={saving}
        onChange={(e) => onToggle({ ...prefs.data!, whatsapp: e.target.checked })}
      />
      {saved ? <p className="text-xs text-ok">{t('portal.settings.saved')}</p> : null}
    </div>
  )
}
