import { useState } from 'react'

import { ErrorState, LoadingState, useT } from '@hikrad/shared'

import {
  createProvider,
  listProviders,
  updateProvider,
  type PaymentProvider,
} from '../../api/paymentProviders'
import { useAuth } from '../../auth/AuthContext'
import { PERM_PAYMENT_PROVIDERS_MANAGE } from '../../auth/permissions'
import { Button } from '../../components/Button'
import { Checkbox, Field, Textarea, TextInput } from '../../components/form'
import { PageHeader } from '../../components/PageHeader'
import { useToast } from '../../components/Toast'
import { useAsync } from '../../hooks/useAsync'

/**
 * Payment provider catalog (v2-2, C1, FR-77.1): name + instructions template
 * only, no API fields — every manager reads it to configure their own
 * account against it; only payment_providers.manage may create/edit.
 * Mirrors CurrencyRatesPage's table pattern.
 */
export function ProviderCatalogPage() {
  const t = useT()
  const { can } = useAuth()
  const canManage = can(PERM_PAYMENT_PROVIDERS_MANAGE)
  const { data, loading, error, reload } = useAsync(() => listProviders(), [])

  return (
    <section>
      <PageHeader title={t('paymentProviders.title')} subtitle={t('paymentProviders.subtitle')} />

      <div className="rounded-md border border-surface-sunken">
        {canManage ? <CreateProviderForm onCreated={reload} /> : null}

        {error ? (
          <ErrorState onRetry={reload} />
        ) : loading ? (
          <LoadingState />
        ) : (data?.items.length ?? 0) === 0 ? (
          <p className="p-6 text-center text-sm text-ink-muted">{t('paymentProviders.empty')}</p>
        ) : (
          <ul className="divide-y divide-surface-sunken">
            {(data?.items ?? []).map((p) => (
              <ProviderRow key={p.id} provider={p} canManage={canManage} onSaved={reload} />
            ))}
          </ul>
        )}
      </div>
    </section>
  )
}

function CreateProviderForm({ onCreated }: { onCreated: () => void }) {
  const t = useT()
  const { toast } = useToast()
  const [name, setName] = useState('')
  const [instructions, setInstructions] = useState('')
  const [busy, setBusy] = useState(false)

  async function submit() {
    setBusy(true)
    try {
      await createProvider(name.trim(), instructions.trim())
      toast(t('paymentProviders.created'), 'ok')
      setName('')
      setInstructions('')
      onCreated()
    } catch (err) {
      toast(err instanceof Error ? err.message : t('common.error.body'), 'danger')
    } finally {
      setBusy(false)
    }
  }

  return (
    <form
      onSubmit={(e) => {
        e.preventDefault()
        void submit()
      }}
      className="grid gap-3 border-b border-surface-sunken p-3 sm:grid-cols-3 sm:items-end"
    >
      <Field label={t('paymentProviders.name')} htmlFor="provider-name">
        <TextInput
          id="provider-name"
          value={name}
          onChange={(e) => setName(e.target.value)}
          required
        />
      </Field>
      <Field label={t('paymentProviders.instructions')} htmlFor="provider-instructions">
        <TextInput
          id="provider-instructions"
          value={instructions}
          onChange={(e) => setInstructions(e.target.value)}
        />
      </Field>
      <Button type="submit" disabled={busy || !name.trim()}>
        {busy ? t('ui.working') : t('paymentProviders.add')}
      </Button>
    </form>
  )
}

function ProviderRow({
  provider,
  canManage,
  onSaved,
}: {
  provider: PaymentProvider
  canManage: boolean
  onSaved: () => void
}) {
  const t = useT()
  const { toast } = useToast()
  const [editing, setEditing] = useState(false)
  const [name, setName] = useState(provider.name)
  const [instructions, setInstructions] = useState(provider.instructions_template)
  const [enabled, setEnabled] = useState(provider.enabled)
  const [busy, setBusy] = useState(false)

  async function save() {
    setBusy(true)
    try {
      await updateProvider(provider.id, {
        name: name.trim(),
        instructions_template: instructions.trim(),
        enabled,
      })
      toast(t('settings.saved'), 'ok')
      setEditing(false)
      onSaved()
    } catch (err) {
      toast(err instanceof Error ? err.message : t('common.error.body'), 'danger')
    } finally {
      setBusy(false)
    }
  }

  if (!editing) {
    return (
      <li className="flex items-center justify-between gap-3 p-3">
        <div>
          <p className="font-medium">{provider.name}</p>
          {!provider.enabled ? (
            <span className="rounded bg-warn/10 px-2 py-0.5 text-xs text-warn">
              {t('paymentProviders.disabled')}
            </span>
          ) : null}
        </div>
        {canManage ? (
          <Button size="sm" variant="ghost" onClick={() => setEditing(true)}>
            {t('ui.edit')}
          </Button>
        ) : null}
      </li>
    )
  }

  return (
    <li className="space-y-3 p-3">
      <Field label={t('paymentProviders.name')}>
        <TextInput value={name} onChange={(e) => setName(e.target.value)} />
      </Field>
      <Field label={t('paymentProviders.instructions')}>
        <Textarea rows={2} value={instructions} onChange={(e) => setInstructions(e.target.value)} />
      </Field>
      <Checkbox
        label={t('paymentProviders.enabled')}
        checked={enabled}
        onChange={(e) => setEnabled(e.target.checked)}
      />
      <div className="flex justify-end gap-2">
        <Button variant="ghost" disabled={busy} onClick={() => setEditing(false)}>
          {t('ui.cancel')}
        </Button>
        <Button disabled={busy || !name.trim()} onClick={() => void save()}>
          {busy ? t('ui.working') : t('ui.save')}
        </Button>
      </div>
    </li>
  )
}
