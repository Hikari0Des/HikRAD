import { useRef } from 'react'

import { LoadingState, useT } from '@hikrad/shared'

import { useAuth } from '../../auth/AuthContext'
import { PERM_SETTINGS_EDIT } from '../../auth/permissions'
import { Button } from '../../components/Button'
import { Field, TextInput } from '../../components/form'
import { useSettingsGroup } from './useSettingsGroup'

const MAX_LOGO_BYTES = 512 * 1024

/** Branding settings (FR-53.1): ISP name/colors + a logo with icon preview. */
export function BrandingSettings() {
  const t = useT()
  const { can } = useAuth()
  const canEdit = can(PERM_SETTINGS_EDIT)
  const g = useSettingsGroup('branding')
  const fileRef = useRef<HTMLInputElement>(null)

  if (!g.loaded) return <LoadingState />

  const v = g.values as {
    name?: string
    logo_url?: string | null
    primary_color?: string
    secondary_color?: string
  }

  async function submit() {
    await g.save(
      {
        name: v.name ?? 'HikRAD',
        logo_url: v.logo_url ?? null,
        primary_color: v.primary_color ?? '#08748f',
        secondary_color: v.secondary_color ?? '#0f172a',
      },
      t('settings.saved'),
      t('common.error.body'),
    )
  }

  function onLogoChosen(e: React.ChangeEvent<HTMLInputElement>) {
    const file = e.target.files?.[0]
    if (!file) return
    if (file.size > MAX_LOGO_BYTES) {
      g.setField('logo_url', v.logo_url ?? null)
      return
    }
    const reader = new FileReader()
    reader.onload = () => g.setField('logo_url', reader.result as string)
    reader.readAsDataURL(file)
  }

  return (
    <div className="max-w-md space-y-4">
      <Field label={t('settings.branding.name')} error={g.errors.name}>
        <TextInput
          disabled={!canEdit}
          value={v.name ?? ''}
          onChange={(e) => g.setField('name', e.target.value)}
        />
      </Field>
      <Field
        label={t('settings.branding.logo')}
        hint={t('settings.branding.logoHint')}
        error={g.errors.logo_url}
      >
        <div className="flex items-center gap-3">
          <div className="flex h-14 w-14 shrink-0 items-center justify-center overflow-hidden rounded-md border border-surface-sunken bg-surface">
            {v.logo_url ? (
              <img
                src={v.logo_url}
                alt={t('settings.branding.logo')}
                className="h-full w-full object-contain"
              />
            ) : (
              <span className="text-xs text-ink-muted">{t('settings.branding.noLogo')}</span>
            )}
          </div>
          {canEdit ? (
            <>
              <input
                ref={fileRef}
                type="file"
                accept="image/png,image/svg+xml,image/jpeg"
                className="hidden"
                onChange={onLogoChosen}
              />
              <Button variant="secondary" size="sm" onClick={() => fileRef.current?.click()}>
                {t('settings.branding.upload')}
              </Button>
              {v.logo_url ? (
                <Button variant="ghost" size="sm" onClick={() => g.setField('logo_url', null)}>
                  {t('ui.remove')}
                </Button>
              ) : null}
            </>
          ) : null}
        </div>
      </Field>
      <Field label={t('settings.branding.primaryColor')} error={g.errors.primary_color}>
        <input
          type="color"
          disabled={!canEdit}
          value={v.primary_color ?? '#08748f'}
          onChange={(e) => g.setField('primary_color', e.target.value)}
          className="h-9 w-16 rounded border border-surface-sunken"
        />
      </Field>
      <Field label={t('settings.branding.secondaryColor')} error={g.errors.secondary_color}>
        <input
          type="color"
          disabled={!canEdit}
          value={v.secondary_color ?? '#0f172a'}
          onChange={(e) => g.setField('secondary_color', e.target.value)}
          className="h-9 w-16 rounded border border-surface-sunken"
        />
      </Field>
      {canEdit ? (
        <Button disabled={g.saving} onClick={() => void submit()}>
          {g.saving ? t('ui.working') : t('ui.save')}
        </Button>
      ) : null}
    </div>
  )
}
