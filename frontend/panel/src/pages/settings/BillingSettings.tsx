import { useState } from 'react'

import { ErrorState, LoadingState, useT } from '@hikrad/shared'

import { ApiError } from '../../api/client'
import { useAuth } from '../../auth/AuthContext'
import { PERM_SETTINGS_EDIT } from '../../auth/permissions'
import { Button } from '../../components/Button'
import { Checkbox, Field, Select, TextInput } from '../../components/form'
import { useSettingsGroup } from './useSettingsGroup'

/** Billing defaults (FR-53.1) + card-payment types/cooldown (FR-59 amendment). */
export function BillingSettings() {
  const t = useT()
  const { can } = useAuth()
  const canEdit = can(PERM_SETTINGS_EDIT)
  const g = useSettingsGroup('billing')

  if (!g.loaded) return <LoadingState />

  const v = g.values as {
    renewal_anchor?: string
    admin_balance_bypass?: boolean
    receipt_prefix?: string
    receipt_branding?: boolean
    voucher_prefix?: string
    receipt_numerals?: string
  }

  async function submit() {
    await g.save(
      {
        renewal_anchor: v.renewal_anchor ?? 'from_expiry',
        admin_balance_bypass: v.admin_balance_bypass ?? false,
        receipt_prefix: v.receipt_prefix ?? '',
        receipt_branding: v.receipt_branding ?? true,
        voucher_prefix: v.voucher_prefix ?? '',
        receipt_numerals: v.receipt_numerals ?? 'auto',
      },
      t('settings.saved'),
      t('common.error.body'),
    )
  }

  return (
    <div className="max-w-md space-y-4">
      <Field
        label={t('settings.billing.renewalAnchor')}
        hint={t('settings.billing.renewalAnchorHint')}
        error={g.errors.renewal_anchor}
      >
        <Select
          disabled={!canEdit}
          value={v.renewal_anchor ?? 'from_expiry'}
          onChange={(e) => g.setField('renewal_anchor', e.target.value)}
        >
          <option value="from_expiry">{t('settings.billing.anchor.fromExpiry')}</option>
          <option value="from_now">{t('settings.billing.anchor.fromNow')}</option>
        </Select>
      </Field>
      <Checkbox
        label={t('settings.billing.adminBypass')}
        description={t('settings.billing.adminBypassHint')}
        disabled={!canEdit}
        checked={v.admin_balance_bypass ?? false}
        onChange={(e) => g.setField('admin_balance_bypass', e.target.checked)}
      />
      <Field label={t('settings.billing.receiptPrefix')} error={g.errors.receipt_prefix}>
        <TextInput
          dir="ltr"
          disabled={!canEdit}
          value={v.receipt_prefix ?? ''}
          onChange={(e) => g.setField('receipt_prefix', e.target.value)}
        />
      </Field>
      <Field label={t('settings.billing.voucherPrefix')} error={g.errors.voucher_prefix}>
        <TextInput
          dir="ltr"
          disabled={!canEdit}
          value={v.voucher_prefix ?? ''}
          onChange={(e) => g.setField('voucher_prefix', e.target.value)}
        />
      </Field>
      <Field label={t('settings.billing.receiptNumerals')} error={g.errors.receipt_numerals}>
        <Select
          disabled={!canEdit}
          value={v.receipt_numerals ?? 'auto'}
          onChange={(e) => g.setField('receipt_numerals', e.target.value)}
        >
          <option value="auto">{t('settings.billing.numerals.auto')}</option>
          <option value="latn">{t('settings.billing.numerals.latn')}</option>
          <option value="arab">{t('settings.billing.numerals.arab')}</option>
        </Select>
      </Field>
      <Checkbox
        label={t('settings.billing.receiptBranding')}
        disabled={!canEdit}
        checked={v.receipt_branding ?? true}
        onChange={(e) => g.setField('receipt_branding', e.target.checked)}
      />
      {canEdit ? (
        <Button disabled={g.saving} onClick={() => void submit()}>
          {g.saving ? t('ui.working') : t('ui.save')}
        </Button>
      ) : null}

      <CardPaymentSettings />
    </div>
  )
}

/**
 * Card-payment types (v2-2 amendment: the reject_cooldown_days field is
 * retired — FR-78.3's trialEligible state machine replaced FR-59.4's fixed
 * cooldown entirely, so the setting it once configured no longer exists).
 * Backend key prefix is `card_payments.*`; until the settings group is
 * wired up GET/PUT 404 with `not_found` and this section shows a pending
 * notice.
 */
function CardPaymentSettings() {
  const t = useT()
  const { can } = useAuth()
  const canEdit = can(PERM_SETTINGS_EDIT)
  const g = useSettingsGroup('card_payments')
  const pending = g.loadError instanceof ApiError && g.loadError.status === 404

  const types = (g.values.types as string[] | undefined) ?? []

  async function submit(typesText: string) {
    await g.save(
      {
        types: typesText
          .split(',')
          .map((s) => s.trim())
          .filter(Boolean),
      },
      t('settings.saved'),
      t('common.error.body'),
    )
  }

  if (!g.loaded) return null

  if (pending) {
    return (
      <div className="mt-8 border-t border-surface-sunken pt-6">
        <h2 className="mb-2 text-sm font-semibold">{t('settings.billing.cardPayments')}</h2>
        <ErrorState
          title={t('settings.billing.cardPaymentsPending')}
          body={t('settings.billing.cardPaymentsPendingBody')}
        />
      </div>
    )
  }

  return (
    <CardPaymentFields
      canEdit={canEdit}
      initialTypes={types.join(', ')}
      saving={g.saving}
      errors={g.errors}
      onSubmit={submit}
    />
  )
}

function CardPaymentFields({
  canEdit,
  initialTypes,
  saving,
  errors,
  onSubmit,
}: {
  canEdit: boolean
  initialTypes: string
  saving: boolean
  errors: Record<string, string>
  onSubmit: (typesText: string) => void
}) {
  const t = useT()
  const [typesText, setTypesText] = useState(initialTypes)

  return (
    <div className="mt-8 border-t border-surface-sunken pt-6">
      <h2 className="mb-3 text-sm font-semibold">{t('settings.billing.cardPayments')}</h2>
      <div className="space-y-4">
        <Field
          label={t('settings.billing.cardTypes')}
          hint={t('settings.billing.cardTypesHint')}
          error={errors.types}
        >
          <TextInput
            dir="ltr"
            disabled={!canEdit}
            value={typesText}
            onChange={(e) => setTypesText(e.target.value)}
          />
        </Field>
        {canEdit ? (
          <Button disabled={saving} onClick={() => onSubmit(typesText)}>
            {saving ? t('ui.working') : t('ui.save')}
          </Button>
        ) : null}
      </div>
    </div>
  )
}
