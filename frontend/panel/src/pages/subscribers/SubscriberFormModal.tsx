import { useEffect, useState } from 'react'

import { useT } from '@hikrad/shared'

import { createSubscriber, updateSubscriber } from '../../api/subscribers'
import type { ManagerView } from '../../api/managers'
import { SERVICE_TYPES } from '../../api/types'
import type {
  MacLockMode,
  NasScope,
  Profile,
  ServiceType,
  Subscriber,
  SubscriberStatus,
  SubscriberWrite,
} from '../../api/types'
import { ApiError, type FieldError } from '../../api/client'
import { Button } from '../../components/Button'
import { Modal } from '../../components/Modal'
import { NasScopePicker } from '../../components/NasScopePicker'
import {
  Checkbox,
  Field,
  RadioGroup,
  RadioOption,
  Select,
  TextInput,
  Textarea,
} from '../../components/form'
import { useToast } from '../../components/Toast'

const STATUSES: SubscriberStatus[] = ['active', 'expired', 'disabled']
const MAC_MODES: MacLockMode[] = ['off', 'learn', 'fixed']

/** iso → value for <input type="datetime-local"> (local time, minutes precision). */
function toLocalInput(iso: string | null): string {
  if (!iso) return ''
  const d = new Date(iso)
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`
}

/**
 * Create/edit a subscriber (FR-1). The WhatsApp-consent toggle sits next to the
 * phone (FR-1.5); the service-type radio carries FR-61 and the NAS pickers the
 * FR-64 scope. Field-level validation errors from the C2 envelope render inline
 * against their field.
 */
export function SubscriberFormModal({
  open,
  onOpenChange,
  existing,
  profiles,
  managers,
  onSaved,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  existing?: Subscriber
  profiles: Profile[]
  managers: ManagerView[]
  onSaved: (result: { subscriber: Subscriber; offerDisconnect?: boolean }) => void
}) {
  const t = useT()
  const { toast } = useToast()
  const editing = Boolean(existing)

  const [form, setForm] = useState(() => initialForm(existing))
  const [errors, setErrors] = useState<Record<string, string>>({})
  const [busy, setBusy] = useState(false)

  // Reset the form whenever the modal opens for a (different) subject.
  const openKey = `${open}:${existing?.id ?? 'new'}`
  useEffect(() => {
    if (open) {
      setForm(initialForm(existing))
      setErrors({})
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [openKey])

  const set = <K extends keyof FormState>(key: K, value: FormState[K]) =>
    setForm((f) => ({ ...f, [key]: value }))

  async function submit() {
    setBusy(true)
    setErrors({})
    try {
      const body = toWrite(form, editing)
      if (editing && existing) {
        const res = await updateSubscriber(existing.id, body)
        toast(t('subscriber.saved'), 'ok')
        onSaved({ subscriber: res.subscriber, offerDisconnect: res.offer_disconnect })
      } else {
        const created = await createSubscriber(body)
        toast(t('subscriber.created'), 'ok')
        onSaved({ subscriber: created })
      }
    } catch (err) {
      if (err instanceof ApiError && err.fieldErrors.length > 0) {
        setErrors(fieldErrorMap(err.fieldErrors))
      } else if (err instanceof ApiError && err.code === 'conflict') {
        setErrors({ username: t('subscriber.usernameTaken') })
      } else {
        toast(err instanceof Error ? err.message : t('common.error.body'), 'danger')
      }
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal
      open={open}
      onOpenChange={busy ? () => {} : onOpenChange}
      size="lg"
      title={editing ? t('subscriber.editTitle') : t('subscriber.createTitle')}
    >
      <div className="grid gap-3 sm:grid-cols-2">
        <Field label={t('subscriber.username')} error={errors.username} htmlFor="sub-username">
          <TextInput
            id="sub-username"
            value={form.username}
            disabled={editing}
            dir="ltr"
            autoCapitalize="none"
            onChange={(e) => set('username', e.target.value)}
          />
        </Field>
        <Field
          label={editing ? t('subscriber.resetPassword') : t('subscriber.password')}
          error={errors.password}
          hint={editing ? t('subscriber.passwordHint') : undefined}
          htmlFor="sub-password"
        >
          <TextInput
            id="sub-password"
            type="text"
            value={form.password}
            dir="ltr"
            disabled={form.noPassword}
            onChange={(e) => set('password', e.target.value)}
          />
        </Field>
        <div className="sm:col-span-2">
          {/* Item 13: hotspot logins may deliberately carry password="". */}
          <Checkbox
            label={t('subscriber.noPassword')}
            description={t('subscriber.noPasswordHint')}
            checked={form.noPassword}
            onChange={(e) => set('noPassword', e.target.checked)}
          />
        </div>
        <Field label={t('subscriber.name')} error={errors.name} htmlFor="sub-name">
          <TextInput
            id="sub-name"
            value={form.name}
            onChange={(e) => set('name', e.target.value)}
          />
        </Field>
        <Field label={t('subscriber.phone')} error={errors.phone} htmlFor="sub-phone">
          <TextInput
            id="sub-phone"
            value={form.phone}
            dir="ltr"
            inputMode="tel"
            onChange={(e) => set('phone', e.target.value)}
          />
        </Field>
        <Field label={t('subscriber.email')} error={errors.email} htmlFor="sub-email">
          <TextInput
            id="sub-email"
            type="email"
            value={form.email}
            dir="ltr"
            onChange={(e) => set('email', e.target.value)}
          />
        </Field>
        <div className="sm:col-span-2">
          <Checkbox
            label={t('subscriber.whatsappOptIn')}
            description={t('subscriber.whatsappHint')}
            checked={form.whatsappOptIn}
            onChange={(e) => set('whatsappOptIn', e.target.checked)}
          />
          {errors.whatsapp_opt_in ? (
            <p role="alert" className="mt-1 text-xs text-danger">
              {errors.whatsapp_opt_in}
            </p>
          ) : null}
        </div>
        <Field label={t('subscriber.status')} error={errors.status} htmlFor="sub-status">
          <Select
            id="sub-status"
            value={form.status}
            onChange={(e) => set('status', e.target.value)}
          >
            {STATUSES.map((s) => (
              <option key={s} value={s}>
                {t(`common.status.${s}`)}
              </option>
            ))}
          </Select>
        </Field>
        <Field label={t('subscriber.profile')} error={errors.profile_id} htmlFor="sub-profile">
          <Select
            id="sub-profile"
            value={form.profileId}
            onChange={(e) => set('profileId', e.target.value)}
          >
            <option value="">{t('ui.none')}</option>
            {profiles
              .filter((p) => !p.archived || p.id === form.profileId)
              .map((p) => (
                <option key={p.id} value={p.id}>
                  {p.name}
                </option>
              ))}
          </Select>
        </Field>
        <Field label={t('subscriber.expiry')} error={errors.expires_at} htmlFor="sub-expiry">
          <TextInput
            id="sub-expiry"
            type="datetime-local"
            value={form.expiresAt}
            dir="ltr"
            onChange={(e) => set('expiresAt', e.target.value)}
          />
        </Field>
        {managers.length > 0 && (
          <Field label={t('subscriber.owner')} error={errors.owner_manager_id} htmlFor="sub-owner">
            <Select
              id="sub-owner"
              value={form.ownerId}
              onChange={(e) => set('ownerId', e.target.value)}
            >
              <option value="">{t('ui.none')}</option>
              {managers.map((m) => (
                <option key={m.id} value={m.id}>
                  {m.username}
                </option>
              ))}
            </Select>
          </Field>
        )}
        <Field label={t('subscriber.macLock')} error={errors.mac_lock_mode} htmlFor="sub-mac-lock">
          <Select
            id="sub-mac-lock"
            value={form.macLockMode}
            onChange={(e) => set('macLockMode', e.target.value)}
          >
            {MAC_MODES.map((m) => (
              <option key={m} value={m}>
                {t(`subscriber.macMode.${m}`)}
              </option>
            ))}
          </Select>
        </Field>
        <Field
          label={t('subscriber.staticIp')}
          error={errors.static_ip}
          hint={t('subscriber.staticIpHint')}
          htmlFor="sub-static-ip"
        >
          <TextInput
            id="sub-static-ip"
            value={form.staticIp}
            dir="ltr"
            onChange={(e) => set('staticIp', e.target.value)}
          />
        </Field>
        <Field
          label={t('subscriber.rateOverride')}
          error={errors.rate_override}
          hint={t('subscriber.rateOverrideHint')}
          htmlFor="sub-rate-override"
        >
          <TextInput
            id="sub-rate-override"
            value={form.rateOverride}
            dir="ltr"
            onChange={(e) => set('rateOverride', e.target.value)}
          />
        </Field>
        <Field
          label={t('subscriber.sessionLimitOverride')}
          error={errors.session_limit_override}
          htmlFor="sub-session-limit-override"
        >
          <TextInput
            id="sub-session-limit-override"
            type="number"
            value={form.sessionLimitOverride}
            dir="ltr"
            onChange={(e) => set('sessionLimitOverride', e.target.value)}
          />
        </Field>
        <Field
          label={t('subscriber.priceOverride')}
          hint={t('subscriber.priceOverrideHint')}
          error={errors.price_override}
          htmlFor="sub-price-override"
        >
          <TextInput
            id="sub-price-override"
            type="number"
            value={form.priceOverride}
            dir="ltr"
            onChange={(e) => set('priceOverride', e.target.value)}
          />
        </Field>
        <Field label={t('subscriber.address')} error={errors.address} htmlFor="sub-address">
          <TextInput
            id="sub-address"
            value={form.address}
            onChange={(e) => set('address', e.target.value)}
          />
        </Field>
        <div className="sm:col-span-2">
          <Field label={t('subscriber.notes')} error={errors.notes} htmlFor="sub-notes">
            <Textarea
              id="sub-notes"
              rows={2}
              value={form.notes}
              onChange={(e) => set('notes', e.target.value)}
            />
          </Field>
        </div>
        {form.status === 'disabled' && (
          <div className="sm:col-span-2">
            <Field
              label={t('subscriber.disabledReason')}
              error={errors.disabled_reason}
              htmlFor="sub-disabled-reason"
            >
              <TextInput
                id="sub-disabled-reason"
                value={form.disabledReason}
                onChange={(e) => set('disabledReason', e.target.value)}
              />
            </Field>
          </div>
        )}
        <div className="sm:col-span-2">
          <ServiceTypeRadio
            value={form.serviceType}
            onChange={(v) => set('serviceType', v)}
            error={errors.service_type}
          />
        </div>
        <NasScopePicker
          scopes={form.nasScopes}
          onChange={(nasScopes) => setForm((f) => ({ ...f, nasScopes }))}
          errors={errors}
        />
      </div>

      <div className="mt-5 flex justify-end gap-2">
        <Button variant="ghost" disabled={busy} onClick={() => onOpenChange(false)}>
          {t('ui.cancel')}
        </Button>
        <Button disabled={busy} onClick={submit}>
          {busy ? t('ui.working') : t('ui.save')}
        </Button>
      </div>
    </Modal>
  )
}

/**
 * FR-63 service selector. A radio, not a checkbox: the three types are
 * exclusive, and "Hotspot only" is a real choice v1's allow_hotspot tickbox
 * could not express — a checkbox would keep implying "PPPoE, plus hotspot".
 */
function ServiceTypeRadio({
  value,
  onChange,
  error,
}: {
  value: ServiceType
  onChange: (v: ServiceType) => void
  error?: string
}) {
  const t = useT()
  return (
    <Field label={t('subscriber.serviceType')} hint={t('subscriber.serviceTypeHint')} error={error}>
      <RadioGroup
        value={value}
        onValueChange={(v) => onChange(v as ServiceType)}
        name="service_type"
      >
        {SERVICE_TYPES.map((v) => (
          <RadioOption key={v} value={v} label={t(`serviceType.${v}`)} />
        ))}
      </RadioGroup>
    </Field>
  )
}

interface FormState {
  username: string
  password: string
  noPassword: boolean
  name: string
  phone: string
  email: string
  whatsappOptIn: boolean
  address: string
  notes: string
  status: string
  profileId: string
  ownerId: string
  expiresAt: string
  macLockMode: string
  staticIp: string
  rateOverride: string
  sessionLimitOverride: string
  priceOverride: string
  disabledReason: string
  serviceType: ServiceType
  nasScopes: NasScope[]
}

function initialForm(s?: Subscriber): FormState {
  return {
    username: s?.username ?? '',
    password: '',
    noPassword: s ? s.has_password === false : false,
    name: s?.name ?? '',
    phone: s?.phone ?? '',
    email: s?.email ?? '',
    whatsappOptIn: s?.whatsapp_opt_in ?? false,
    address: s?.address ?? '',
    notes: s?.notes ?? '',
    status: s?.status ?? 'active',
    profileId: s?.profile_id ?? '',
    ownerId: s?.owner_manager_id ?? '',
    expiresAt: toLocalInput(s?.expires_at ?? null),
    macLockMode: s?.mac_lock_mode ?? 'off',
    staticIp: s?.static_ip ?? '',
    rateOverride: s?.rate_override ?? '',
    sessionLimitOverride: s?.session_limit_override != null ? String(s.session_limit_override) : '',
    priceOverride: s?.price_override != null ? String(s.price_override) : '',
    disabledReason: s?.disabled_reason ?? '',
    serviceType: s?.service_type ?? 'pppoe',
    nasScopes: s?.nas_scopes ?? [],
  }
}

function toWrite(f: FormState, editing: boolean): SubscriberWrite {
  const body: SubscriberWrite = {
    name: f.name || null,
    phone: f.phone || null,
    email: f.email || null,
    address: f.address || null,
    notes: f.notes || null,
    status: f.status as SubscriberStatus,
    profile_id: f.profileId || null,
    expires_at: f.expiresAt ? new Date(f.expiresAt).toISOString() : null,
    mac_lock_mode: f.macLockMode as MacLockMode,
    static_ip: f.staticIp || null,
    rate_override: f.rateOverride || null,
    session_limit_override: f.sessionLimitOverride ? Number(f.sessionLimitOverride) : null,
    price_override: f.priceOverride ? Number(f.priceOverride) : null,
    disabled_reason: f.status === 'disabled' ? f.disabledReason || null : null,
    service_type: f.serviceType,
    whatsapp_opt_in: f.whatsappOptIn,
    // An empty array is the API's explicit "any NAS" (it clears the set);
    // omitting the field would leave the existing scope in place.
    nas_scopes: f.nasScopes,
    owner_manager_id: f.ownerId || null,
  }
  if (!editing) body.username = f.username
  if (f.noPassword) {
    body.no_password = true
  } else if (f.password) {
    body.password = f.password
  }
  return body
}

function fieldErrorMap(errs: FieldError[]): Record<string, string> {
  const out: Record<string, string> = {}
  for (const e of errs) out[e.field] = e.message
  return out
}
