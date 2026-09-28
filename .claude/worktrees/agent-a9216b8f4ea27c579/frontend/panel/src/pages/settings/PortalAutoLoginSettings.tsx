import { LoadingState, useT } from '@hikrad/shared'

import { useAuth } from '../../auth/AuthContext'
import { PERM_SETTINGS_EDIT } from '../../auth/permissions'
import { Button } from '../../components/Button'
import { Checkbox, TextInput, Textarea } from '../../components/form'
import { FormField } from '../../components/patterns'
import { useSettingsGroup } from './useSettingsGroup'

interface PortalAutoLoginValues {
  enabled?: boolean
  trusted_networks?: string[]
  trusted_proxies?: string[]
  session_ttl_minutes?: number
}

function linesToList(s: string): string[] {
  return s
    .split('\n')
    .map((l) => l.trim())
    .filter(Boolean)
}

/** Connected-client portal auto-login (FR-167, Decision 64). OFF BY DEFAULT
 * — this screen is the one place an operator opts in, and every field here
 * is one of the six required conditions FR-167's threat model names: the
 * feature toggle (condition 1), the trusted-network allowlist (condition
 * 2), the trusted-proxy allowlist that governs whether X-Forwarded-For is
 * ever honoured (condition 3), and the session TTL (the hard cap gate item
 * 11 enforces). Conditions 4-6 (exact-one-session match, MAC corroboration,
 * account status) are not configurable — they are always enforced, by
 * design, with no admin override. */
export function PortalAutoLoginSettings() {
  const t = useT()
  const { can } = useAuth()
  const canEdit = can(PERM_SETTINGS_EDIT)
  const g = useSettingsGroup('portal_auto_login')

  if (!g.loaded) return <LoadingState />

  const v = g.values as PortalAutoLoginValues

  async function submit() {
    await g.save(
      {
        enabled: v.enabled ?? false,
        trusted_networks: v.trusted_networks ?? [],
        trusted_proxies: v.trusted_proxies ?? [],
        session_ttl_minutes: v.session_ttl_minutes ?? 30,
      },
      t('settings.saved'),
      t('common.error.body'),
    )
  }

  return (
    <div className="max-w-lg space-y-4">
      <p className="rounded-md border border-warn/40 bg-warn/10 p-3 text-sm text-ink">
        {t('settings.portalAutoLogin.warning')}
      </p>

      <Checkbox
        label={t('settings.portalAutoLogin.enable')}
        description={t('settings.portalAutoLogin.enableHint')}
        disabled={!canEdit}
        checked={v.enabled ?? false}
        onChange={(e) => g.setField('enabled', e.target.checked)}
      />

      <FormField
        label={t('settings.portalAutoLogin.trustedNetworks')}
        hint={t('settings.portalAutoLogin.trustedNetworksHint')}
        error={g.errors.trusted_networks}
      >
        {(id) => (
          <Textarea
            id={id}
            dir="ltr"
            rows={4}
            disabled={!canEdit}
            value={(v.trusted_networks ?? []).join('\n')}
            onChange={(e) => g.setField('trusted_networks', linesToList(e.target.value))}
            placeholder="10.0.0.0/8"
          />
        )}
      </FormField>

      <FormField
        label={t('settings.portalAutoLogin.trustedProxies')}
        hint={t('settings.portalAutoLogin.trustedProxiesHint')}
        error={g.errors.trusted_proxies}
      >
        {(id) => (
          <Textarea
            id={id}
            dir="ltr"
            rows={2}
            disabled={!canEdit}
            value={(v.trusted_proxies ?? []).join('\n')}
            onChange={(e) => g.setField('trusted_proxies', linesToList(e.target.value))}
            placeholder="127.0.0.1/32"
          />
        )}
      </FormField>

      <FormField
        label={t('settings.portalAutoLogin.sessionTtl')}
        hint={t('settings.portalAutoLogin.sessionTtlHint')}
        error={g.errors.session_ttl_minutes}
      >
        {(id) => (
          <TextInput
            id={id}
            type="number"
            dir="ltr"
            min={1}
            disabled={!canEdit}
            value={v.session_ttl_minutes ?? 30}
            onChange={(e) => g.setField('session_ttl_minutes', Number(e.target.value))}
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
