import { useState } from 'react'

import { LoadingState, useT } from '@hikrad/shared'

import { listGatewayConfigs, putGatewayConfig, type GatewayConfig } from '../../api/gateways'
import { useAuth } from '../../auth/AuthContext'
import { PERM_GATEWAYS_MANAGE } from '../../auth/permissions'
import { Button } from '../../components/Button'
import { Checkbox, Field, TextInput } from '../../components/form'
import { useToast } from '../../components/Toast'
import { useAsync } from '../../hooks/useAsync'

const KNOWN_GATEWAYS = ['mock', 'zaincash']

/** Payment gateway enable/config (FR-23, contract C3) — D's Phase-4 admin API. */
export function GatewaySettings() {
  const t = useT()
  const { can } = useAuth()
  const canEdit = can(PERM_GATEWAYS_MANAGE)
  const { data, loading, reload } = useAsync(() => listGatewayConfigs(), [])

  if (loading) return <LoadingState />

  const byName = new Map((data?.items ?? []).map((g) => [g.gateway, g]))
  const gateways = KNOWN_GATEWAYS.map(
    (name) =>
      byName.get(name) ?? {
        gateway: name,
        enabled: false,
        mode: 'live' as const,
        configured: false,
      },
  )

  return (
    <div className="max-w-lg space-y-4">
      {gateways.map((gw) => (
        <GatewayRow key={gw.gateway} gateway={gw} canEdit={canEdit} onSaved={reload} />
      ))}
      <p className="text-xs text-ink-muted">{t('settings.gateways.hint')}</p>
    </div>
  )
}

function GatewayRow({
  gateway,
  canEdit,
  onSaved,
}: {
  gateway: GatewayConfig
  canEdit: boolean
  onSaved: () => void
}) {
  const t = useT()
  const { toast } = useToast()
  const [enabled, setEnabled] = useState(gateway.enabled)
  const [creds, setCreds] = useState<Record<string, string>>({})
  const [busy, setBusy] = useState(false)
  const needsCreds = gateway.gateway !== 'mock'

  async function submit() {
    setBusy(true)
    try {
      await putGatewayConfig(gateway.gateway, {
        enabled,
        mode: gateway.mode,
        creds: Object.values(creds).some(Boolean) ? creds : undefined,
      })
      toast(t('settings.saved'), 'ok')
      onSaved()
    } catch (err) {
      toast(err instanceof Error ? err.message : t('common.error.body'), 'danger')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="rounded-md border border-surface-sunken bg-surface-raised p-4">
      <div className="flex items-center justify-between">
        <span className="font-medium capitalize">{gateway.gateway}</span>
        {gateway.configured ? (
          <span className="rounded bg-ok/10 px-2 py-0.5 text-xs text-ok">
            {t('settings.gateways.configured')}
          </span>
        ) : needsCreds ? (
          <span className="rounded bg-warn/10 px-2 py-0.5 text-xs text-warn">
            {t('settings.gateways.notConfigured')}
          </span>
        ) : null}
      </div>
      <Checkbox
        label={t('settings.gateways.enabled')}
        disabled={!canEdit}
        checked={enabled}
        onChange={(e) => setEnabled(e.target.checked)}
      />
      {needsCreds ? (
        <div className="mt-2 grid gap-2 sm:grid-cols-2">
          <Field label={t('settings.gateways.merchantId')}>
            <TextInput
              dir="ltr"
              disabled={!canEdit}
              placeholder={gateway.configured ? '••••••••' : ''}
              onChange={(e) => setCreds((c) => ({ ...c, msisdn: e.target.value }))}
            />
          </Field>
          <Field label={t('settings.gateways.secretKey')}>
            <TextInput
              type="password"
              dir="ltr"
              disabled={!canEdit}
              placeholder={gateway.configured ? '••••••••' : ''}
              onChange={(e) => setCreds((c) => ({ ...c, secret: e.target.value }))}
            />
          </Field>
        </div>
      ) : null}
      {canEdit ? (
        <Button size="sm" className="mt-3" disabled={busy} onClick={() => void submit()}>
          {busy ? t('ui.working') : t('ui.save')}
        </Button>
      ) : null}
    </div>
  )
}
