import { LoadingState, useT } from '@hikrad/shared'

import { useAuth } from '../../auth/AuthContext'
import { PERM_SETTINGS_EDIT } from '../../auth/permissions'
import { Button } from '../../components/Button'
import { Field, Select, TextInput } from '../../components/form'
import { useSettingsGroup } from './useSettingsGroup'

/** Locale settings (FR-53.1): timezone/currency/date format/default language. */
export function LocaleSettings() {
  const t = useT()
  const { can } = useAuth()
  const canEdit = can(PERM_SETTINGS_EDIT)
  const g = useSettingsGroup('locale')

  if (!g.loaded) return <LoadingState />

  const v = g.values as {
    timezone?: string
    currency?: string
    date_format?: string
    language?: string
  }

  async function submit() {
    await g.save(
      {
        timezone: v.timezone ?? 'Asia/Baghdad',
        currency: v.currency ?? 'IQD',
        date_format: v.date_format ?? 'DD/MM/YYYY',
        language: v.language ?? 'ar',
      },
      t('settings.saved'),
      t('common.error.body'),
    )
  }

  return (
    <div className="max-w-md space-y-4">
      <Field label={t('settings.locale.timezone')} error={g.errors.timezone}>
        <TextInput
          dir="ltr"
          disabled={!canEdit}
          value={v.timezone ?? 'Asia/Baghdad'}
          onChange={(e) => g.setField('timezone', e.target.value)}
        />
      </Field>
      <Field label={t('settings.locale.currency')} error={g.errors.currency}>
        <TextInput
          dir="ltr"
          disabled={!canEdit}
          value={v.currency ?? 'IQD'}
          onChange={(e) => g.setField('currency', e.target.value)}
        />
      </Field>
      <Field label={t('settings.locale.dateFormat')} error={g.errors.date_format}>
        <Select
          disabled={!canEdit}
          value={v.date_format ?? 'DD/MM/YYYY'}
          onChange={(e) => g.setField('date_format', e.target.value)}
        >
          {/* i18n-exempt: date-format tokens, not UI copy */}
          <option value="DD/MM/YYYY">DD/MM/YYYY</option>
          {/* i18n-exempt: date-format tokens, not UI copy */}
          <option value="YYYY-MM-DD">YYYY-MM-DD</option>
          {/* i18n-exempt: date-format tokens, not UI copy */}
          <option value="MM/DD/YYYY">MM/DD/YYYY</option>
        </Select>
      </Field>
      <Field label={t('settings.locale.language')} error={g.errors.language}>
        <Select
          disabled={!canEdit}
          value={v.language ?? 'ar'}
          onChange={(e) => g.setField('language', e.target.value)}
        >
          <option value="ar">{t('languages.ar')}</option>
          <option value="en">{t('languages.en')}</option>
          <option value="ku">{t('languages.ku')}</option>
        </Select>
      </Field>
      {canEdit ? (
        <Button disabled={g.saving} onClick={() => void submit()}>
          {g.saving ? t('ui.working') : t('ui.save')}
        </Button>
      ) : null}
    </div>
  )
}
