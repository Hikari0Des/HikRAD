import { useState } from 'react'

import { ErrorState, LoadingState, useT } from '@hikrad/shared'

import {
  listInstanceMethodSettings,
  listInstanceProviderAccounts,
  listMethodSettings,
  listProviderAccounts,
  listProviders,
  putInstanceMethodSetting,
  putInstanceProviderAccount,
  putMethodSetting,
  putProviderAccount,
  type MethodSetting,
  type PaymentProvider,
  type ProviderAccount,
} from '../../api/paymentProviders'
import { useAuth } from '../../auth/AuthContext'
import { PERM_PAYMENT_PROVIDERS_MANAGE } from '../../auth/permissions'
import { Button } from '../../components/Button'
import { Checkbox, Field, TextInput } from '../../components/form'
import { PageHeader } from '../../components/PageHeader'
import { useToast } from '../../components/Toast'
import { useAsync } from '../../hooks/useAsync'

const BUILTIN_METHODS = ['scratch_card', 'voucher'] as const

/** The write half of one settings scope (a manager's own rows, or the instance defaults). */
interface MethodScope {
  putSetting: (methodKey: string, enabled: boolean) => Promise<MethodSetting>
  putAccount: (
    providerId: string,
    accountDetails: string,
    instructionsOverride?: string,
  ) => Promise<ProviderAccount>
}

/**
 * A manager's own receiving accounts + method toggles (v2-2, C2/C3, FR-77.2/
 * 77.3) — every manager needs this, not just admins, so it's not gated
 * behind payment_providers.manage the way the catalog CRUD is. Absence of a
 * row means disabled (C3's "no row = off"); a subscriber never sees a
 * provider their manager hasn't both enabled AND configured an account for
 * (kickoff blocker 1, C4).
 *
 * Admins additionally see the INSTANCE DEFAULTS section (owner decision
 * 2026-07-19, migration 0592): the methods a subscriber with no owning
 * manager resolves. Same rules, instance-scoped rows.
 */
export function MyPaymentMethodsPage() {
  const t = useT()
  const { manager, can } = useAuth()
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

  const isAdmin = can(PERM_PAYMENT_PROVIDERS_MANAGE)
  const { data: instAccounts, reload: reloadInstAccounts } = useAsync(
    () => (isAdmin ? listInstanceProviderAccounts() : Promise.resolve({ items: [] })),
    [isAdmin],
  )
  const { data: instSettings, reload: reloadInstSettings } = useAsync(
    () => (isAdmin ? listInstanceMethodSettings() : Promise.resolve({ items: [] })),
    [isAdmin],
  )

  if (!manager) return null
  if (providersError) return <ErrorState />
  if (providersLoading || accountsLoading || settingsLoading) return <LoadingState />

  const enabledProviders = (providers?.items ?? []).filter((p) => p.enabled)
  const managerScope: MethodScope = {
    putSetting: (key, enabled) => putMethodSetting(manager.id, key, enabled),
    putAccount: (providerId, details, instructions) =>
      putProviderAccount(manager.id, providerId, details, instructions),
  }
  const instanceScope: MethodScope = {
    putSetting: putInstanceMethodSetting,
    putAccount: putInstanceProviderAccount,
  }

  return (
    <section>
      <PageHeader title={t('myPaymentMethods.title')} subtitle={t('myPaymentMethods.subtitle')} />

      <ScopeSection
        providers={enabledProviders}
        accounts={accounts?.items ?? []}
        settings={settings?.items ?? []}
        scope={managerScope}
        onSaved={() => {
          reloadAccounts()
          reloadSettings()
        }}
      />

      {isAdmin ? (
        <>
          <h2 className="mb-1 mt-8 text-base font-semibold">
            {t('myPaymentMethods.instanceTitle')}
          </h2>
          <p className="mb-3 text-sm text-ink-muted">{t('myPaymentMethods.instanceHint')}</p>
          <ScopeSection
            providers={enabledProviders}
            accounts={instAccounts?.items ?? []}
            settings={instSettings?.items ?? []}
            scope={instanceScope}
            onSaved={() => {
              reloadInstAccounts()
              reloadInstSettings()
            }}
          />
        </>
      ) : null}
    </section>
  )
}

function ScopeSection({
  providers,
  accounts,
  settings,
  scope,
  onSaved,
}: {
  providers: PaymentProvider[]
  accounts: ProviderAccount[]
  settings: MethodSetting[]
  scope: MethodScope
  onSaved: () => void
}) {
  const t = useT()
  const accountByProvider = new Map(accounts.map((a) => [a.provider_id, a]))
  const enabledByKey = new Map(settings.map((s) => [s.method_key, s.enabled]))

  return (
    <div className="space-y-4">
      <div className="rounded-md border border-surface-sunken p-4">
        <h2 className="mb-3 text-sm font-semibold">{t('myPaymentMethods.builtinTitle')}</h2>
        <div className="space-y-2">
          {BUILTIN_METHODS.map((key) => (
            <MethodToggle
              key={key}
              methodKey={key}
              label={t(`myPaymentMethods.builtin.${key}`)}
              enabled={enabledByKey.get(key) ?? false}
              scope={scope}
              onSaved={onSaved}
            />
          ))}
        </div>
      </div>

      {providers.length === 0 ? (
        <p className="text-sm text-ink-muted">{t('myPaymentMethods.noProviders')}</p>
      ) : (
        providers.map((p) => (
          <ProviderAccountCard
            // The account list can arrive after first render (instance scope
            // loads lazily) — keying on the stored details re-seeds the form
            // when it does.
            key={`${p.id}:${accountByProvider.get(p.id)?.account_details ?? ''}`}
            provider={p}
            account={accountByProvider.get(p.id) ?? null}
            enabled={enabledByKey.get(p.id) ?? false}
            scope={scope}
            onSaved={onSaved}
          />
        ))
      )}
    </div>
  )
}

function MethodToggle({
  methodKey,
  label,
  enabled,
  scope,
  onSaved,
}: {
  methodKey: string
  label: string
  enabled: boolean
  scope: MethodScope
  onSaved: () => void
}) {
  const t = useT()
  const { toast } = useToast()
  const [busy, setBusy] = useState(false)

  async function toggle(next: boolean) {
    setBusy(true)
    try {
      await scope.putSetting(methodKey, next)
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
  provider,
  account,
  enabled,
  scope,
  onSaved,
}: {
  provider: PaymentProvider
  account: ProviderAccount | null
  enabled: boolean
  scope: MethodScope
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
      await scope.putAccount(provider.id, details.trim(), instructions.trim())
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
      await scope.putSetting(provider.id, next)
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
