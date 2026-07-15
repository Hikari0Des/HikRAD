import { useState } from 'react'

import { LoadingState, useT } from '@hikrad/shared'

import { getHealth } from '../../api/monitoring'
import { useAuth } from '../../auth/AuthContext'
import { PERM_SETTINGS_EDIT } from '../../auth/permissions'
import { Button } from '../../components/Button'
import { Checkbox, Field, TextInput } from '../../components/form'
import { useAsync } from '../../hooks/useAsync'
import { useSettingsGroup } from './useSettingsGroup'

/** Remote access / Cloudflare tunnel (FR-57, contract C7): off by default. */
export function RemoteAccessSettings() {
  const t = useT()
  const { can } = useAuth()
  const canEdit = can(PERM_SETTINGS_EDIT)
  const g = useSettingsGroup('remote_access')
  const health = useAsync(() => getHealth(), [])
  const [token, setToken] = useState('')

  if (!g.loaded) return <LoadingState />

  const v = g.values as { enabled?: boolean; token_set?: boolean }
  const tunnelState = health.data?.tunnel?.state

  async function submit() {
    const body: Record<string, unknown> = { enabled: v.enabled ?? false }
    if (token) body.token = token
    const ok = await g.save(body, t('settings.saved'), t('common.error.body'))
    if (ok) setToken('')
  }

  return (
    <div className="max-w-md space-y-4">
      <div className="rounded-md border border-surface-sunken bg-surface-raised p-3 text-sm">
        <span className="font-medium">{t('settings.remoteAccess.status')}: </span>
        {tunnelState ? (
          <span
            className={
              tunnelState === 'connected'
                ? 'text-ok'
                : tunnelState === 'disconnected'
                  ? 'text-danger'
                  : 'text-ink-muted'
            }
          >
            {t(`settings.remoteAccess.state.${tunnelState}`)}
          </span>
        ) : (
          <span className="text-ink-muted">{t('settings.remoteAccess.state.unknown')}</span>
        )}
      </div>
      <Checkbox
        label={t('settings.remoteAccess.enable')}
        description={t('settings.remoteAccess.enableHint')}
        disabled={!canEdit}
        checked={v.enabled ?? false}
        onChange={(e) => g.setField('enabled', e.target.checked)}
      />
      <Field
        label={t('settings.remoteAccess.token')}
        hint={
          v.token_set ? t('settings.remoteAccess.tokenSet') : t('settings.remoteAccess.tokenHint')
        }
        error={g.errors.token}
      >
        <TextInput
          type="password"
          dir="ltr"
          disabled={!canEdit}
          value={token}
          placeholder={v.token_set ? '••••••••' : ''}
          onChange={(e) => setToken(e.target.value)}
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
