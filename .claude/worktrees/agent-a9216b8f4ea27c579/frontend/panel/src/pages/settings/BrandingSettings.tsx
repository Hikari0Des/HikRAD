import { forwardRef, useImperativeHandle, useRef, useState } from 'react'

import { LoadingState, useT } from '@hikrad/shared'

import { deleteBrandingLogo, uploadBrandingLogo } from '../../api/branding'
import { ApiError } from '../../api/client'
import { useAuth } from '../../auth/AuthContext'
import { PERM_SETTINGS_EDIT } from '../../auth/permissions'
import { Button } from '../../components/Button'
import { TextInput } from '../../components/form'
import { FormField } from '../../components/patterns'
import { useToast } from '../../components/Toast'
import { useSettingsGroup } from './useSettingsGroup'

/** Branding settings (FR-53.1, v2 phase 11 FR-91): ISP name/colors + a
 * disk-backed logo with icon preview. The logo uploads/deletes immediately
 * through its own endpoint (contract C3) — it is server-managed and
 * rejected by this screen's own name/color save (see
 * validateServerManagedFields on the backend), so it is never part of the
 * `submit()` body below. */
/** Imperative handle so a host (the first-run wizard) can persist the name/colors
 * from its own Next button — FR-106.2, fixing the branding-name-not-saved bug. */
export interface BrandingSettingsHandle {
  save: () => Promise<void>
}

export const BrandingSettings = forwardRef<BrandingSettingsHandle, { hideSaveButton?: boolean }>(
  function BrandingSettings({ hideSaveButton }, ref) {
    return <BrandingSettingsInner hideSaveButton={hideSaveButton} handleRef={ref} />
  },
)

function BrandingSettingsInner({
  hideSaveButton,
  handleRef,
}: {
  hideSaveButton?: boolean
  handleRef: React.ForwardedRef<BrandingSettingsHandle>
}) {
  const t = useT()
  const { can } = useAuth()
  const { toast } = useToast()
  const canEdit = can(PERM_SETTINGS_EDIT)
  const g = useSettingsGroup('branding')
  const fileRef = useRef<HTMLInputElement>(null)
  const [uploading, setUploading] = useState(false)

  // Expose save() to a host (the wizard) — invoked every render so it always
  // closes over the latest values, never a stale snapshot.
  useImperativeHandle(handleRef, () => ({ save: submit }))

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
      <FormField label={t('settings.branding.name')} error={g.errors.name}>
        {(id) => (
          <TextInput
            id={id}
            disabled={!canEdit}
            value={v.name ?? ''}
            onChange={(e) => g.setField('name', e.target.value)}
          />
        )}
      </FormField>
      <FormField
        label={t('settings.branding.logo')}
        hint={t('settings.branding.logoHint')}
        error={g.errors.logo_url}
      >
        {(id) => (
          <div className="flex items-center gap-3">
            <div className="flex h-14 w-14 shrink-0 items-center justify-center overflow-hidden rounded-md border border-border bg-surface">
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
                  id={id}
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
        )}
      </FormField>
      <FormField label={t('settings.branding.primaryColor')} error={g.errors.primary_color}>
        {(id) => (
          <input
            id={id}
            type="color"
            disabled={!canEdit}
            value={v.primary_color ?? '#08748f'}
            onChange={(e) => g.setField('primary_color', e.target.value)}
            className="h-9 w-16 rounded border border-border"
          />
        )}
      </FormField>
      <FormField label={t('settings.branding.secondaryColor')} error={g.errors.secondary_color}>
        {(id) => (
          <input
            id={id}
            type="color"
            disabled={!canEdit}
            value={v.secondary_color ?? '#0f172a'}
            onChange={(e) => g.setField('secondary_color', e.target.value)}
            className="h-9 w-16 rounded border border-border"
          />
        )}
      </FormField>
      {canEdit && !hideSaveButton ? (
        <Button disabled={g.saving} onClick={() => void submit()}>
          {g.saving ? t('ui.working') : t('ui.save')}
        </Button>
      ) : null}
    </div>
  )
}
