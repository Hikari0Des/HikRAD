import { useState } from 'react'

import { ErrorState, LoadingState, useT } from '@hikrad/shared'

import {
  listMethodSettings,
  listProviderAccounts,
  listProviders,
  putMethodSetting,
  putProviderAccount,
  type PaymentProvider,
  type ProviderAccount,
} from '../../api/paymentProviders'
import { useAuth } from '../../auth/AuthContext'
import { Button } from '../../components/Button'
import { Checkbox, Field, TextInput } from '../../components/form'
import { PageHeader } from '../../components/PageHeader'
import { useToast } from '../../components/Toast'
import { useAsync } from '../../hooks/useAsync'

const BUILTIN_METHODS = ['scratch_card', 'voucher'] as const

/**
 * A manager's own receiving accounts + method toggles (v2-2, C2/C3, FR-77.2/
 * 77.3) — every manager needs this, not just admins, so it's not gated
 * behind payment_providers.manage the way the catalog CRUD is. Absence of a
 * row means disabled (C3's "no row = off"); a subscriber never sees a
 * provider their manager hasn't both enabled AND configured an account for
 * (kickoff blocker 1, C4).
 */
export function MyPaymentMethodsPage() {
  const t = useT()
  const { manager } = useAuth()
  const {
    data: providers,
    loading: providersLoading,
    error: providersError,
  } = useAsync(() => listProviders(), [])
  const {
    data: accounts,
    loading: accountsLoading,
    reload: reloadAccounts,
  } = useAsync(
    () => (manager ? listProviderAccounts(manager.id) : Promise.resolve({ items: [] })),
    [manager?.id],
  )
  const {
    data: settings,
    loading: settingsLoading,
    reload: reloadSettings,
  } = useAsync(
    () => (manager ? listMethodSettings(manager.id) : Promise.resolve({ items: [] })),
    [manager?.id],
  )

  if (!manager) return null
  if (providersError) return <ErrorState />
  if (providersLoading || accountsLoading || settingsLoading) return <LoadingState />

  const accountByProvider = new Map((accounts?.items ?? []).map((a) => [a.provider_id, a]))
  const enabledByKey = new Map((settings?.items ?? []).map((s) => [s.method_key, s.enabled]))
  const enabledProviders = (providers?.items ?? []).filter((p) => p.enabled)

  return (
    <section>
      <PageHeader title={t('myPaymentMethods.title')} subtitle={t('myPaymentMethods.subtitle')} />

      <div className="space-y-4">
        <div className="rounded-md border border-surface-sunken p-4">
          <h2 className="mb-3 text-sm font-semibold">{t('myPaymentMethods.builtinTitle')}</h2>
          <div className="space-y-2">
            {BUILTIN_METHODS.map((key) => (
              <MethodToggle
                key={key}
                managerId={manager.id}
                methodKey={key}
                label={t(`myPaymentMethods.builtin.${key}`)}
                enabled={enabledByKey.get(key) ?? false}
                onSaved={reloadSettings}
              />
            ))}
          </div>
        </div>

        {enabledProviders.length === 0 ? (
          <p className="text-sm text-ink-muted">{t('myPaymentMethods.noProviders')}</p>
        ) : (
          enabledProviders.map((p) => (
            <ProviderAccountCard
              key={p.id}
              managerId={manager.id}
              provider={p}
              account={accountByProvider.get(p.id) ?? null}
              enabled={enabledByKey.get(p.id) ?? false}
              onSaved={() => {
                reloadAccounts()
                reloadSettings()
              }}
            />
          ))
        )}
      </div>
    </section>
  )
}

function MethodToggle({
  managerId,
  methodKey,
  label,
  enabled,
  onSaved,
}: {
  managerId: string
  methodKey: string
  label: string
  enabled: boolean
  onSaved: () => void
}) {
  const t = useT()
  const { toast } = useToast()
  const [busy, setBusy] = useState(false)

  async function toggle(next: boolean) {
    setBusy(true)
    try {
      await putMethodSetting(managerId, methodKey, next)
      onSaved()
    } catch (err) {
      toast(err instanceof Error ? err.message : t('common.error.body'), 'danger')
    } finally {
      setBusy(false)
    }
  }

  return (
    <Checkbox
      label={label}
      checked={enabled}
      disabled={busy}
      onChange={(e) => void toggle(e.target.checked)}
    />
  )
}

function ProviderAccountCard({
  managerId,
  provider,
  account,
  enabled,
  onSaved,
}: {
  managerId: string
  provider: PaymentProvider
  account: ProviderAccount | null
  enabled: boolean
  onSaved: () => void
}) {
  const t = useT()
  const { toast } = useToast()
  const [details, setDetails] = useState(account?.account_details ?? '')
  const [instructions, setInstructions] = useState(account?.instructions_override ?? '')
  const [busy, setBusy] = useState(false)
  const [toggling, setToggling] = useState(false)

  async function save() {
    setBusy(true)
    try {
      await putProviderAccount(managerId, provider.id, details.trim(), instructions.trim())
      toast(t('settings.saved'), 'ok')
      onSaved()
    } catch (err) {
      toast(err instanceof Error ? err.message : t('common.error.body'), 'danger')
    } finally {
      setBusy(false)
    }
  }

  async function toggle(next: boolean) {
    setToggling(true)
    try {
      await putMethodSetting(managerId, provider.id, next)
      onSaved()
    } catch (err) {
      toast(err instanceof Error ? err.message : t('common.error.body'), 'danger')
    } finally {
      setToggling(false)
    }
  }

  return (
    <div className="rounded-md border border-surface-sunken p-4">
      <div className="mb-3 flex items-center justify-between">
        <span className="font-medium">{provider.name}</span>
        {enabled && !account ? (
          <span className="rounded bg-warn/10 px-2 py-0.5 text-xs text-warn">
            {t('myPaymentMethods.accountMissing')}
          </span>
        ) : null}
      </div>
      <Checkbox
        label={t('myPaymentMethods.enableProvider')}
        checked={enabled}
        disabled={toggling}
        onChange={(e) => void toggle(e.target.checked)}
      />
      <div className="mt-2 grid gap-2 sm:grid-cols-2">
        <Field
          label={t('myPaymentMethods.accountDetails')}
          hint={t('myPaymentMethods.accountDetailsHint')}
        >
          <TextInput dir="ltr" value={details} onChange={(e) => setDetails(e.target.value)} />
        </Field>
        <Field label={t('myPaymentMethods.instructionsOverride')}>
          <TextInput value={instructions} onChange={(e) => setInstructions(e.target.value)} />
        </Field>
      </div>
      <Button
        className="mt-3"
        size="sm"
        disabled={busy || !details.trim()}
        onClick={() => void save()}
      >
        {busy ? t('ui.working') : t('ui.save')}
      </Button>
    </div>
  )
}
