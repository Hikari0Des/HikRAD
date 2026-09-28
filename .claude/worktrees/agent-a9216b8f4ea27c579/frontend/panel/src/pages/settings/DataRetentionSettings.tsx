import { LoadingState, useT } from '@hikrad/shared'

import { useAuth } from '../../auth/AuthContext'
import { PERM_SETTINGS_EDIT } from '../../auth/permissions'
import { Button } from '../../components/Button'
import { TextInput } from '../../components/form'
import { FormField } from '../../components/patterns'
import { useSettingsGroup } from './useSettingsGroup'

/** Data retention (FR-33): raw/rollup periods, with floor explanations. */
export function DataRetentionSettings() {
  const t = useT()
  const { can } = useAuth()
  const canEdit = can(PERM_SETTINGS_EDIT)
  const g = useSettingsGroup('data_retention')

  if (!g.loaded) return <LoadingState />

  const v = g.values as { raw_months?: number; rollup_years?: number }

  async function submit() {
    await g.save(
      { raw_months: v.raw_months ?? 12, rollup_years: v.rollup_years ?? 3 },
      t('settings.saved'),
      t('common.error.body'),
    )
  }

  return (
    <div className="max-w-md space-y-4">
      <FormField
        label={t('settings.retention.rawMonths')}
        hint={t('settings.retention.rawMonthsFloor')}
        error={g.errors.raw_months}
      >
        {(id) => (
          <TextInput
            id={id}
            type="number"
            dir="ltr"
            min={12}
            disabled={!canEdit}
            value={v.raw_months ?? 12}
            onChange={(e) => g.setField('raw_months', Number(e.target.value))}
          />
        )}
      </FormField>
      <FormField
        label={t('settings.retention.rollupYears')}
        hint={t('settings.retention.rollupYearsFloor')}
        error={g.errors.rollup_years}
      >
        {(id) => (
          <TextInput
            id={id}
            type="number"
            dir="ltr"
            min={3}
            disabled={!canEdit}
            value={v.rollup_years ?? 3}
            onChange={(e) => g.setField('rollup_years', Number(e.target.value))}
          />
        )}
      </FormField>
      {canEdit ? (
        <Button disabled={g.saving} onClick={() => void submit()}>
          {g.saving ? t('ui.working') : t('ui.save')}
        </Button>
      ) : null}
    </div>
  )
}
