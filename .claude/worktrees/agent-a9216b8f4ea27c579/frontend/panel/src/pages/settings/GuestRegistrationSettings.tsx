import { LoadingState, useT } from '@hikrad/shared'

import { listProfiles } from '../../api/profiles'
import { useAuth } from '../../auth/AuthContext'
import { PERM_SETTINGS_EDIT } from '../../auth/permissions'
import { Button } from '../../components/Button'
import { Checkbox, Select, TextInput } from '../../components/form'
import { FormField } from '../../components/patterns'
import { useAsync } from '../../hooks/useAsync'
import { useSettingsGroup } from './useSettingsGroup'

interface GuestRegistrationValues {
  enabled?: boolean
  daily_cap?: number
  passcode_ttl_minutes?: number
  cooldown_minutes?: number
  default_profile_id?: string
}

/** Self-service guest registration (FR-169) + the OTP login it shares its
 * passcode machinery with (FR-172). Setup order (phase brief §3.4) is a
 * documented prerequisite: configure a messaging channel (Settings >
 * Notifications, SMS or WhatsApp) FIRST, then a default plan here, THEN
 * enable — a fresh install has no default plan set, so this screen refuses
 * to let an operator turn the toggle on without one (the register endpoint
 * itself also refuses server-side either way). */
export function GuestRegistrationSettings() {
  const t = useT()
  const { can } = useAuth()
  const canEdit = can(PERM_SETTINGS_EDIT)
  const g = useSettingsGroup('guest_registration')
  const profiles = useAsync(() => listProfiles(), [])

  if (!g.loaded || profiles.loading) return <LoadingState />

  const v = g.values as GuestRegistrationValues

  async function submit() {
    await g.save(
      {
        enabled: v.enabled ?? false,
        daily_cap: v.daily_cap ?? 100,
        passcode_ttl_minutes: v.passcode_ttl_minutes ?? 10,
        cooldown_minutes: v.cooldown_minutes ?? 5,
        default_profile_id: v.default_profile_id ?? '',
      },
      t('settings.saved'),
      t('common.error.body'),
    )
  }

  return (
    <div className="max-w-lg space-y-4">
      <Checkbox
        label={t('settings.guestRegistration.enable')}
        description={t('settings.guestRegistration.enableHint')}
        disabled={!canEdit}
        checked={v.enabled ?? false}
        onChange={(e) => g.setField('enabled', e.target.checked)}
      />

      <FormField
        label={t('settings.guestRegistration.defaultProfile')}
        hint={t('settings.guestRegistration.defaultProfileHint')}
        error={g.errors.default_profile_id}
      >
        {(id) => (
          <Select
            id={id}
            disabled={!canEdit}
            value={v.default_profile_id ?? ''}
            onChange={(e) => g.setField('default_profile_id', e.target.value)}
          >
            <option value="">{t('settings.guestRegistration.defaultProfileNone')}</option>
            {(profiles.data?.items ?? []).map((p) => (
              <option key={p.id} value={p.id}>
                {p.name}
              </option>
            ))}
          </Select>
        )}
      </FormField>

      <FormField
        label={t('settings.guestRegistration.dailyCap')}
        hint={t('settings.guestRegistration.dailyCapHint')}
        error={g.errors.daily_cap}
      >
        {(id) => (
          <TextInput
            id={id}
            type="number"
            dir="ltr"
            min={1}
            disabled={!canEdit}
            value={v.daily_cap ?? 100}
            onChange={(e) => g.setField('daily_cap', Number(e.target.value))}
          />
        )}
      </FormField>

      <FormField
        label={t('settings.guestRegistration.passcodeTtl')}
        error={g.errors.passcode_ttl_minutes}
      >
        {(id) => (
          <TextInput
            id={id}
            type="number"
            dir="ltr"
            min={1}
            disabled={!canEdit}
            value={v.passcode_ttl_minutes ?? 10}
            onChange={(e) => g.setField('passcode_ttl_minutes', Number(e.target.value))}
          />
        )}
      </FormField>

      <FormField label={t('settings.guestRegistration.cooldown')} error={g.errors.cooldown_minutes}>
        {(id) => (
          <TextInput
            id={id}
            type="number"
            dir="ltr"
            min={0}
            disabled={!canEdit}
            value={v.cooldown_minutes ?? 5}
            onChange={(e) => g.setField('cooldown_minutes', Number(e.target.value))}
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
