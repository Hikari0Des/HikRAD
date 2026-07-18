import { useRef, useState } from 'react'

import { LoadingState, useT } from '@hikrad/shared'

import { deleteBrandingLogo, uploadBrandingLogo } from '../../api/branding'
import { ApiError } from '../../api/client'
import { useAuth } from '../../auth/AuthContext'
import { PERM_SETTINGS_EDIT } from '../../auth/permissions'
import { Button } from '../../components/Button'
import { Field, TextInput } from '../../components/form'
import { useToast } from '../../components/Toast'
import { useSettingsGroup } from './useSettingsGroup'

/** Branding settings (FR-53.1, v2 phase 11 FR-91): ISP name/colors + a
 * disk-backed logo with icon preview. The logo uploads/deletes immediately
 * through its own endpoint (contract C3) — it is server-managed and
 * rejected by this screen's own name/color save (see
 * validateServerManagedFields on the backend), so it is never part of the
 * `submit()` body below. */
export function BrandingSettings() {
  const t = useT()
  const { can } = useAuth()
  const { toast } = useToast()
  const canEdit = can(PERM_SETTINGS_EDIT)
  const g = useSettingsGroup('branding')
  const fileRef = useRef<HTMLInputElement>(null)
  const [uploading, setUploading] = useState(false)

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
        primary_color: v.primary_color ?? '#08748f',
        secondary_color: v.secondary_color ?? '#0f172a',
      },
      t('settings.saved'),
      t('common.error.body'),
    )
  }

  async function onLogoChosen(e: React.ChangeEvent<HTMLInputElement>) {
    const file = e.target.files?.[0]
    if (fileRef.current) fileRef.current.value = ''
    if (!file) return
    setUploading(true)
    try {
      const res = await uploadBrandingLogo(file)
      g.setField('logo_url', res.logo_url ?? null)
      toast(t('settings.saved'), 'ok')
    } catch (err) {
      const message =
        err instanceof ApiError && err.fieldErrors.length > 0
          ? err.fieldErrors[0].message
          : t('common.error.body')
      toast(message, 'danger')
    } finally {
      setUploading(false)
    }
  }

  async function onRemoveLogo() {
    setUploading(true)
    try {
      await deleteBrandingLogo()
      g.setField('logo_url', null)
      toast(t('settings.saved'), 'ok')
    } catch {
      toast(t('common.error.body'), 'danger')
    } finally {
      setUploading(false)
    }
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
                onChange={(e) => void onLogoChosen(e)}
              />
              <Button
                variant="secondary"
                size="sm"
                disabled={uploading}
                onClick={() => fileRef.current?.click()}
              >
                {uploading ? t('ui.working') : t('settings.branding.upload')}
              </Button>
              {v.logo_url ? (
                <Button
                  variant="ghost"
                  size="sm"
                  disabled={uploading}
                  onClick={() => void onRemoveLogo()}
                >
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
