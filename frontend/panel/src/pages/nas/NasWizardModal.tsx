import { useEffect, useState } from 'react'

import { Ltr, useT } from '@hikrad/shared'

import { createNas, discoverNasServices, updateNas } from '../../api/nas'
import { ApiError, type FieldError } from '../../api/client'
import type { Nas, NasHealthFinding, NasServiceWrite, NasType, NasWrite } from '../../api/types'
import { Button } from '../../components/Button'
import { Modal } from '../../components/Modal'
import { Checkbox, Field, Select, TextInput } from '../../components/form'
import { useToast } from '../../components/Toast'

const TYPES: NasType[] = ['pppoe', 'hotspot']

export interface NasPrefill {
  name?: string
  ip?: string
  ros_version?: string
}

/**
 * NAS create/edit wizard (FR-14). Two steps: step 1 identity (name/IP + the
 * FR-62 service instances this router runs), step 2 connection (shared secret,
 * CoA port, ROS version, SNMP community, location). Discovery pre-fills step 1.
 * On edit the secret/SNMP fields are left blank and only rotated when typed.
 */
export function NasWizardModal({
  open,
  onOpenChange,
  existing,
  prefill,
  onSaved,
}: {
  open: boolean
  onOpenChange: (o: boolean) => void
  existing?: Nas
  prefill?: NasPrefill
  onSaved: () => void
}) {
  const t = useT()
  const { toast } = useToast()
  const editing = Boolean(existing)

  const [step, setStep] = useState(1)
  const [form, setForm] = useState(() => initial(existing, prefill))
  const [errors, setErrors] = useState<Record<string, string>>({})
  const [busy, setBusy] = useState(false)

  const openKey = `${open}:${existing?.id ?? 'new'}:${prefill?.ip ?? ''}`
  useEffect(() => {
    if (open) {
      setForm(initial(existing, prefill))
      setErrors({})
      setStep(1)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [openKey])

  const set = <K extends keyof NasForm>(k: K, v: NasForm[K]) => setForm((f) => ({ ...f, [k]: v }))

  async function save() {
    setBusy(true)
    setErrors({})
    try {
      const body = toWrite(form, editing)
      if (editing && existing) await updateNas(existing.id, body)
      else await createNas(body)
      toast(editing ? t('nas.saved') : t('nas.created'), 'ok')
      onSaved()
    } catch (err) {
      if (err instanceof ApiError && err.fieldErrors.length > 0) {
        setErrors(mapErrors(err.fieldErrors))
        setStep(1)
      } else if (err instanceof ApiError && err.code === 'conflict') {
        setErrors({ ip: t('nas.ipTaken') })
        setStep(1)
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
      title={editing ? t('nas.editTitle') : t('nas.createTitle')}
      description={t('nas.wizardStep', { step, total: 2 })}
    >
      {step === 1 ? (
        <div className="space-y-3">
          <Field label={t('nas.name')} error={errors.name}>
            <TextInput value={form.name} onChange={(e) => set('name', e.target.value)} />
          </Field>
          <Field label={t('nas.ip')} error={errors.ip}>
            <TextInput value={form.ip} dir="ltr" onChange={(e) => set('ip', e.target.value)} />
          </Field>
          <ServicesEditor
            services={form.services}
            onChange={(services) => set('services', services)}
            errors={errors}
            nasId={existing?.id}
            hasApiCreds={Boolean(existing?.has_api_creds)}
          />
          <div className="flex justify-end gap-2 pt-2">
            <Button variant="ghost" onClick={() => onOpenChange(false)}>
              {t('ui.cancel')}
            </Button>
            <Button onClick={() => setStep(2)}>{t('ui.next')}</Button>
          </div>
        </div>
      ) : (
        <div className="space-y-3">
          <Field
            label={t('nas.secret')}
            error={errors.secret}
            hint={editing ? t('nas.secretEditHint') : t('nas.secretHint')}
          >
            <TextInput
              value={form.secret}
              dir="ltr"
              onChange={(e) => set('secret', e.target.value)}
            />
          </Field>
          <div className="grid gap-3 sm:grid-cols-2">
            <Field label={t('nas.coaPort')} error={errors.coa_port}>
              <TextInput
                type="number"
                dir="ltr"
                value={form.coaPort}
                onChange={(e) => set('coaPort', e.target.value)}
              />
            </Field>
            <Field label={t('nas.rosVersion')} error={errors.ros_version}>
              <Select value={form.rosVersion} onChange={(e) => set('rosVersion', e.target.value)}>
                <option value="">{t('ui.unknown')}</option>
                <option value="7">7</option>
                <option value="6">6</option>
              </Select>
            </Field>
          </div>
          <Field label={t('nas.location')} error={errors.location}>
            <TextInput value={form.location} onChange={(e) => set('location', e.target.value)} />
          </Field>
          <Field
            label={t('nas.snmp')}
            error={errors.snmp_community}
            hint={editing ? t('nas.snmpEditHint') : t('nas.snmpHint')}
          >
            <TextInput value={form.snmp} dir="ltr" onChange={(e) => set('snmp', e.target.value)} />
          </Field>
          <label className="flex items-center gap-2 text-sm">
            <input
              type="checkbox"
              checked={form.enabled}
              onChange={(e) => set('enabled', e.target.checked)}
            />
            {t('nas.enabled')}
          </label>

          <div className="border-t border-surface-sunken pt-3">
            <p className="mb-2 text-xs font-medium text-ink-muted">
              {t('nas.autoSetup.credsTitle')}
            </p>
            <div className="grid gap-3 sm:grid-cols-2">
              <Field label={t('nas.autoSetup.apiUser')} error={errors.api_user}>
                <TextInput
                  value={form.apiUser}
                  dir="ltr"
                  onChange={(e) => set('apiUser', e.target.value)}
                />
              </Field>
              <Field label={t('nas.autoSetup.apiPort')} error={errors.api_port}>
                <TextInput
                  type="number"
                  dir="ltr"
                  value={form.apiPort}
                  onChange={(e) => set('apiPort', e.target.value)}
                />
              </Field>
            </div>
            <Field
              label={t('nas.autoSetup.apiPassword')}
              hint={
                editing && existing?.has_api_creds
                  ? t('nas.autoSetup.apiPasswordSetHint')
                  : undefined
              }
              error={errors.api_password}
            >
              <TextInput
                type="password"
                dir="ltr"
                value={form.apiPassword}
                placeholder={editing && existing?.has_api_creds ? '••••••••' : ''}
                onChange={(e) => set('apiPassword', e.target.value)}
              />
            </Field>
          </div>

          <div className="flex justify-between gap-2 pt-2">
            <Button variant="ghost" onClick={() => setStep(1)}>
              {t('ui.back')}
            </Button>
            <Button disabled={busy} onClick={save}>
              {busy ? t('ui.working') : t('ui.save')}
            </Button>
          </div>
        </div>
      )}
    </Modal>
  )
}

/**
 * FR-62/63 services sub-list: the router's PPPoE server and each hotspot zone.
 * The array is the whole truth for the NAS — removing a row here deletes that
 * instance — so removing the last one is refused (a NAS with no service can
 * authenticate nobody).
 *
 * ros_server_name matters more than it looks: it is how an Access-Request is
 * matched to its zone (C7). With one hotspot it can stay empty (the sole
 * instance resolves unambiguously); with several, an unnamed zone cannot be
 * told apart from its neighbours, so the hint says so and the backend rejects
 * duplicates.
 */
function ServicesEditor({
  services,
  onChange,
  errors,
  nasId,
  hasApiCreds,
}: {
  services: NasServiceWrite[]
  onChange: (next: NasServiceWrite[]) => void
  errors: Record<string, string>
  nasId?: string
  hasApiCreds: boolean
}) {
  const t = useT()
  const { toast } = useToast()
  const [detecting, setDetecting] = useState(false)
  // Health findings persist in the form rather than passing through a toast:
  // each one is a standing router misconfiguration with a command attached, and
  // an operator needs to read it while typing, not catch it as it disappears.
  const [health, setHealth] = useState<NasHealthFinding[]>([])
  const patch = (i: number, next: Partial<NasServiceWrite>) =>
    onChange(services.map((s, j) => (i === j ? { ...s, ...next } : s)))

  /**
   * Read the router's real services (FR-62.6) and merge them into the form.
   *
   * Merge, never replace: an instance the NAS already has keeps its id (and so
   * its pool assignment and the subscribers scoped to it) and only refreshes
   * what the router is authoritative for — the server name, interface and
   * enabled state. Anything the router doesn't report is left alone rather than
   * deleted; discovery is not evidence a service is gone, only evidence of what
   * the router answered right now.
   *
   * Nothing is saved here: the operator still reviews and presses Save.
   */
  async function detect() {
    if (!nasId) return
    setDetecting(true)
    try {
      const { items, health: found } = await discoverNasServices(nasId)
      setHealth(found ?? [])
      const byId = new Map(services.map((s) => [s.id, s]))
      const seen = new Set<string>()
      const merged: NasServiceWrite[] = []
      for (const d of items) {
        const existing = d.matched_service_id ? byId.get(d.matched_service_id) : undefined
        if (existing) seen.add(d.matched_service_id)
        merged.push({
          ...(existing ?? {}),
          service: d.service,
          label: existing?.label || d.label,
          interface_note: d.interface_note,
          ros_server_name: d.ros_server_name,
          enabled: d.enabled,
        })
      }
      // Keep rows the router didn't report (e.g. an instance it has powered
      // down, or one added here ahead of the router's config).
      for (const s of services) {
        if (!s.id || !seen.has(s.id)) {
          const alreadyMerged = merged.some(
            (m) =>
              m.service === s.service && (m.ros_server_name ?? '') === (s.ros_server_name ?? ''),
          )
          if (!alreadyMerged) merged.push(s)
        }
      }
      onChange(merged)

      // Surface the router's own pool names: a hotspot whose pool is named here
      // but missing on the router is exactly the "no address from ip pool"
      // login failure, and this is the moment the operator can see both.
      const pools = items
        .filter((d) => d.router_pool_name)
        .map((d) => `${d.ros_server_name || d.label} → ${d.router_pool_name}`)
      toast(
        pools.length > 0
          ? t('nas.detectedWithPools', { count: items.length, pools: pools.join(', ') })
          : t('nas.detected', { count: items.length }),
        'ok',
      )
    } catch (err) {
      if (err instanceof ApiError && err.code === 'no_api_credentials') {
        toast(t('nas.detectNeedsCreds'), 'danger')
      } else {
        toast(err instanceof Error ? err.message : t('common.error.body'), 'danger')
      }
    } finally {
      setDetecting(false)
    }
  }

  return (
    <Field label={t('nas.services')} error={errors.services} hint={t('nas.servicesHint')}>
      <div className="space-y-2">
        {nasId && (
          <div className="flex items-center gap-2">
            <Button
              size="sm"
              variant="secondary"
              disabled={detecting || !hasApiCreds}
              onClick={detect}
            >
              {detecting ? t('ui.working') : t('nas.detectServices')}
            </Button>
            <span className="text-xs text-ink-muted">
              {hasApiCreds ? t('nas.detectHint') : t('nas.detectNeedsCreds')}
            </span>
          </div>
        )}
        {health.map((h) => (
          <div
            key={h.code}
            className="rounded border border-warn/40 bg-warn/10 p-2 text-sm"
            role="status"
          >
            <p className="font-medium">{t(`nas.health.${h.code}`)}</p>
            <p className="mt-0.5 text-ink-muted">{h.detail}</p>
            {/* The router command, verbatim and copyable: HikRAD is telling the
                operator their router is misconfigured, so it owes them the exact
                fix rather than a description of one. LTR — it is a command. */}
            <p className="mt-1 text-xs text-ink-muted">{t('nas.health.fixLabel')}</p>
            <code className="mt-0.5 block overflow-x-auto rounded bg-surface-sunken px-2 py-1 text-xs">
              <Ltr>{h.fix}</Ltr>
            </code>
          </div>
        ))}
        {services.map((s, i) => (
          <div key={s.id ?? `new-${i}`} className="rounded border border-surface-sunken p-2">
            <div className="grid gap-2 sm:grid-cols-2">
              <Select
                aria-label={t('nas.serviceKind')}
                value={s.service}
                onChange={(e) => patch(i, { service: e.target.value as NasType })}
              >
                {TYPES.map((ty) => (
                  <option key={ty} value={ty}>
                    {t(`nas.typeName.${ty}`)}
                  </option>
                ))}
              </Select>
              <TextInput
                aria-label={t('nas.serviceLabel')}
                placeholder={t('nas.serviceLabelPlaceholder')}
                value={s.label ?? ''}
                onChange={(e) => patch(i, { label: e.target.value })}
              />
              <TextInput
                aria-label={t('nas.rosServerName')}
                placeholder={t('nas.rosServerNamePlaceholder')}
                dir="ltr"
                value={s.ros_server_name ?? ''}
                onChange={(e) => patch(i, { ros_server_name: e.target.value })}
              />
              <div className="flex items-center justify-between gap-2">
                <Checkbox
                  label={t('nas.serviceEnabled')}
                  checked={s.enabled ?? true}
                  onChange={(e) => patch(i, { enabled: e.target.checked })}
                />
                <Button
                  size="sm"
                  variant="ghost"
                  disabled={services.length === 1}
                  title={services.length === 1 ? t('nas.serviceLastHint') : undefined}
                  onClick={() => onChange(services.filter((_, j) => j !== i))}
                >
                  {t('ui.remove')}
                </Button>
              </div>
            </div>
            {errors[`services.${i}.ros_server_name`] && (
              <p role="alert" className="mt-1 text-xs text-danger">
                {errors[`services.${i}.ros_server_name`]}
              </p>
            )}
          </div>
        ))}
        <Button
          size="sm"
          variant="secondary"
          onClick={() => onChange([...services, { service: 'hotspot', enabled: true }])}
        >
          {t('nas.addService')}
        </Button>
      </div>
    </Field>
  )
}

interface NasForm {
  name: string
  ip: string
  services: NasServiceWrite[]
  secret: string
  coaPort: string
  rosVersion: string
  location: string
  snmp: string
  enabled: boolean
  apiUser: string
  apiPort: string
  apiPassword: string
}

function initial(n?: Nas, prefill?: NasPrefill): NasForm {
  return {
    name: n?.name ?? prefill?.name ?? '',
    ip: n?.ip ?? prefill?.ip ?? '',
    // A new NAS starts with one PPPoE instance: the shape of the overwhelming
    // majority of routers, and the one every v1 install upgraded into.
    services: n?.services.map((s) => ({
      id: s.id,
      service: s.service,
      label: s.label,
      interface_note: s.interface_note,
      ip_pool_id: s.ip_pool_id,
      ros_server_name: s.ros_server_name,
      enabled: s.enabled,
    })) ?? [{ service: 'pppoe', enabled: true }],
    secret: '',
    coaPort: n?.coa_port != null ? String(n.coa_port) : '3799',
    rosVersion: n?.ros_version ?? prefill?.ros_version ?? '',
    location: n?.location ?? '',
    snmp: '',
    enabled: n?.enabled ?? true,
    apiUser: n?.api_user ?? '',
    apiPort: n?.api_port ? String(n.api_port) : '8728',
    apiPassword: '',
  }
}

function toWrite(f: NasForm, editing: boolean): NasWrite {
  const body: NasWrite = {
    name: f.name,
    ip: f.ip,
    services: f.services,
    coa_port: f.coaPort ? Number(f.coaPort) : 3799,
    ros_version: f.rosVersion || null,
    location: f.location,
    enabled: f.enabled,
  }
  // On create the secret is required; on edit an empty secret leaves it as-is.
  if (f.secret || !editing) body.secret = f.secret
  if (f.snmp) body.snmp_community = f.snmp
  if (f.apiUser) body.api_user = f.apiUser
  if (f.apiPort) body.api_port = Number(f.apiPort)
  if (f.apiPassword) body.api_password = f.apiPassword
  return body
}

function mapErrors(errs: FieldError[]): Record<string, string> {
  const out: Record<string, string> = {}
  for (const e of errs) out[e.field] = e.message
  return out
}
