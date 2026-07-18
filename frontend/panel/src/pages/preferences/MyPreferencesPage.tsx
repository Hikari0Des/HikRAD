import { useEffect, useState } from 'react'

import { ErrorState, LoadingState, THEME_PREFERENCES, useT } from '@hikrad/shared'

import {
  getPreferences,
  putPreferences,
  type NotifChannels,
  type Preferences,
} from '../../api/preferences'
import { ApiError } from '../../api/client'
import { Button } from '../../components/Button'
import { Checkbox, Field, Select, TextInput } from '../../components/form'
import { PageHeader } from '../../components/PageHeader'
import { useToast } from '../../components/Toast'
import { useAsync } from '../../hooks/useAsync'

const LANGUAGES = ['en', 'ar', 'ku'] as const
const NUMERALS = ['auto', 'latn', 'arab'] as const
const TABLE_PAGE_SIZES = [10, 25, 50, 100] as const

/** Every notification_prefs key the C1 contract validates against — the nine
 * monitorsvc alert rule types (reusing their existing alerts.type.* labels)
 * plus FR-80's payment_tickets_all. */
const RULE_TYPES = [
  'nas_down',
  'nas_up',
  'device_down',
  'device_up',
  'radius_reject_spike',
  'acct_backlog',
  'disk_low',
  'expiring_digest',
  'agent_balance_low',
] as const
const NOTIFICATION_KEYS = [...RULE_TYPES, 'payment_tickets_all'] as const

interface FormState {
  language: string
  theme: string
  numerals: string
  landingPage: string
  tablePageSize: string
  notificationPrefs: Record<string, NotifChannels>
}

function toForm(p: Preferences): FormState {
  return {
    language: p.language ?? '',
    theme: p.theme ?? '',
    numerals: p.numerals ?? '',
    landingPage: p.landing_page ?? '',
    tablePageSize: p.table_page_size ? String(p.table_page_size) : '',
    notificationPrefs: p.notification_prefs ?? {},
  }
}

function toBody(f: FormState): Preferences {
  return {
    language: f.language as Preferences['language'],
    theme: f.theme as Preferences['theme'],
    numerals: f.numerals as Preferences['numerals'],
    landing_page: f.landingPage,
    table_page_size: f.tablePageSize ? Number(f.tablePageSize) : 0,
    notification_prefs: f.notificationPrefs,
  }
}

/**
 * Self-service preferences (v2-6, FR-84): language/theme/numerals/landing
 * page/table page size/notification channels, all self-only (the endpoint
 * carries no id) and presentation-only — nothing here ever feeds a
 * permission check or a monetary figure. Full-document PUT, so save always
 * submits the whole form it just loaded via GET.
 */
export function MyPreferencesPage() {
  const t = useT()
  const { toast } = useToast()
  const q = useAsync(getPreferences, [])
  const [form, setForm] = useState<FormState | null>(null)
  const [errors, setErrors] = useState<Record<string, string>>({})
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    if (q.data) setForm(toForm(q.data))
  }, [q.data])

  async function save(next: FormState) {
    setBusy(true)
    setErrors({})
    try {
      const saved = await putPreferences(toBody(next))
      setForm(toForm(saved))
      toast(t('preferences.saved'), 'ok')
    } catch (err) {
      if (err instanceof ApiError && err.fieldErrors.length > 0) {
        const map: Record<string, string> = {}
        for (const fe of err.fieldErrors) map[fe.field] = fe.message
        setErrors(map)
      } else {
        toast(err instanceof Error ? err.message : t('common.error.body'), 'danger')
      }
    } finally {
      setBusy(false)
    }
  }

  if (q.error) return <ErrorState onRetry={q.reload} />
  if (q.loading || !form) return <LoadingState />

  const set = <K extends keyof FormState>(key: K, value: FormState[K]) =>
    setForm((f) => (f ? { ...f, [key]: value } : f))

  function setChannel(key: string, channel: keyof NotifChannels, value: boolean) {
    setForm((f) => {
      if (!f) return f
      const current = f.notificationPrefs[key] ?? { in_app: false, push: false }
      return {
        ...f,
        notificationPrefs: { ...f.notificationPrefs, [key]: { ...current, [channel]: value } },
      }
    })
  }

  return (
    <section className="space-y-6">
      <PageHeader title={t('preferences.title')} subtitle={t('preferences.hint')} />

      <div className="grid gap-4 rounded-md border border-surface-sunken bg-surface-raised p-4 sm:grid-cols-2">
        <Field label={t('preferences.language')} error={errors.language}>
          <Select value={form.language} onChange={(e) => set('language', e.target.value)}>
            <option value="">{t('preferences.unsetOption')}</option>
            {LANGUAGES.map((l) => (
              <option key={l} value={l}>
                {t(`languages.${l}`)}
              </option>
            ))}
          </Select>
        </Field>
        <Field label={t('preferences.theme')} error={errors.theme}>
          <Select value={form.theme} onChange={(e) => set('theme', e.target.value)}>
            <option value="">{t('preferences.unsetOption')}</option>
            {THEME_PREFERENCES.map((th) => (
              <option key={th} value={th}>
                {t(`common.theme.${th}`)}
              </option>
            ))}
          </Select>
        </Field>
        <Field
          label={t('preferences.numerals')}
          hint={t('preferences.numeralsHint')}
          error={errors.numerals}
        >
          <Select value={form.numerals} onChange={(e) => set('numerals', e.target.value)}>
            <option value="">{t('preferences.unsetOption')}</option>
            {NUMERALS.map((n) => (
              <option key={n} value={n}>
                {t(`preferences.numeralsValue.${n}`)}
              </option>
            ))}
          </Select>
        </Field>
        <Field label={t('preferences.tablePageSize')} error={errors.table_page_size}>
          <Select value={form.tablePageSize} onChange={(e) => set('tablePageSize', e.target.value)}>
            <option value="">{t('preferences.unsetOption')}</option>
            {TABLE_PAGE_SIZES.map((n) => (
              <option key={n} value={n}>
                {n}
              </option>
            ))}
          </Select>
        </Field>
        <div className="sm:col-span-2">
          <Field
            label={t('preferences.landingPage')}
            hint={t('preferences.landingPageHint')}
            error={errors.landing_page}
          >
            <TextInput
              value={form.landingPage}
              dir="ltr"
              placeholder={t('preferences.landingPagePlaceholder')}
              onChange={(e) => set('landingPage', e.target.value)}
            />
          </Field>
        </div>
      </div>

      <div className="rounded-md border border-surface-sunken bg-surface-raised p-4">
        <h2 className="mb-1 text-sm font-semibold">{t('preferences.notifications')}</h2>
        <p className="mb-3 text-xs text-ink-muted">{t('preferences.notificationsHint')}</p>
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead className="text-start text-xs text-ink-muted">
              <tr>
                <th className="px-2 py-1.5 text-start font-medium">{t('preferences.ruleType')}</th>
                <th className="px-2 py-1.5 text-center font-medium">
                  {t('preferences.channelInApp')}
                </th>
                <th className="px-2 py-1.5 text-center font-medium">
                  {t('preferences.channelPush')}
                </th>
              </tr>
            </thead>
            <tbody>
              {NOTIFICATION_KEYS.map((key) => {
                const ch = form.notificationPrefs[key] ?? { in_app: false, push: false }
                return (
                  <tr key={key} className="border-t border-surface-sunken/60">
                    <td className="px-2 py-2">
                      {key === 'payment_tickets_all'
                        ? t('preferences.paymentTicketsAll')
                        : t(`alerts.type.${key}`)}
                    </td>
                    <td className="px-2 py-2 text-center">
                      <Checkbox
                        aria-label={t('preferences.channelInApp')}
                        checked={ch.in_app}
                        onChange={(e) => setChannel(key, 'in_app', e.target.checked)}
                      />
                    </td>
                    <td className="px-2 py-2 text-center">
                      <Checkbox
                        aria-label={t('preferences.channelPush')}
                        checked={ch.push}
                        onChange={(e) => setChannel(key, 'push', e.target.checked)}
                      />
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        </div>
      </div>

      <div className="flex justify-end gap-2">
        <Button variant="ghost" disabled={busy} onClick={() => void save(toForm({}))}>
          {t('preferences.reset')}
        </Button>
        <Button disabled={busy} onClick={() => void save(form)}>
          {busy ? t('ui.working') : t('preferences.save')}
        </Button>
      </div>
    </section>
  )
}
