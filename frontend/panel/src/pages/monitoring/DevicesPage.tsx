import { useState } from 'react'
import { Link } from 'react-router-dom'

import { ErrorState, LoadingState, useT } from '@hikrad/shared'

import {
  createDevice,
  deleteDevice,
  listDevices,
  updateDevice,
  type Device,
  type DeviceType,
  type DeviceWrite,
} from '../../api/monitoring'
import { ApiError } from '../../api/client'
import { useAuth } from '../../auth/AuthContext'
import { PERM_MONITORING_EDIT } from '../../auth/permissions'
import { Button } from '../../components/Button'
import { ConfirmDialog } from '../../components/ConfirmDialog'
import { Checkbox, Field, Select, TextInput } from '../../components/form'
import { Modal } from '../../components/Modal'
import { PageHeader } from '../../components/PageHeader'
import { useToast } from '../../components/Toast'
import { useAsync } from '../../hooks/useAsync'
import { StatusBadgeMon } from './StatusView'

const TYPES: DeviceType[] = ['ap', 'switch', 'router', 'server', 'other']

/**
 * Monitored-device CRUD + status cards (FR-60). Devices are visually distinct
 * from NASes — no RADIUS affordances — but reuse the status-page components.
 */
export function DevicesPage() {
  const t = useT()
  const { can } = useAuth()
  const q = useAsync(() => listDevices(), [])
  const canEdit = can(PERM_MONITORING_EDIT)
  const [editing, setEditing] = useState<Device | null | 'new'>(null)
  const [deleting, setDeleting] = useState<Device | null>(null)
  const { toast } = useToast()

  if (q.error) return <ErrorState onRetry={q.reload} />

  async function doDelete() {
    if (!deleting) return
    await deleteDevice(deleting.id)
    toast(t('devices.deleted'), 'ok')
    q.reload()
  }

  return (
    <section>
      <PageHeader
        title={t('devices.title')}
        subtitle={t('devices.subtitle')}
        actions={
          canEdit ? (
            <Button size="sm" onClick={() => setEditing('new')}>
              {t('devices.create')}
            </Button>
          ) : null
        }
      />

      {q.loading || !q.data ? (
        <LoadingState />
      ) : q.data.items.length === 0 ? (
        <p className="rounded-md border border-dashed border-surface-sunken p-8 text-center text-sm text-ink-muted">
          {t('devices.empty')}
        </p>
      ) : (
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3">
          {q.data.items.map((d) => (
            <div
              key={d.id}
              className="rounded-md border border-surface-sunken bg-surface-raised p-4"
            >
              <div className="flex items-start justify-between">
                <div>
                  <Link
                    to={`/devices/${d.id}/status`}
                    className="font-semibold hover:text-brand-strong"
                  >
                    {d.name}
                  </Link>
                  <p className="text-xs text-ink-muted">
                    <bdi dir="ltr">{d.ip}</bdi> · {t(`devices.type.${d.type}`)}
                  </p>
                  {d.location ? <p className="text-xs text-ink-muted">{d.location}</p> : null}
                </div>
                <StatusBadgeMon status={d.status} />
              </div>
              {canEdit ? (
                <div className="mt-3 flex gap-1">
                  <Button size="sm" variant="ghost" onClick={() => setEditing(d)}>
                    {t('ui.edit')}
                  </Button>
                  <Button size="sm" variant="ghost" onClick={() => setDeleting(d)}>
                    {t('ui.delete')}
                  </Button>
                </div>
              ) : null}
            </div>
          ))}
        </div>
      )}

      {editing !== null ? (
        <DeviceFormModal
          existing={editing === 'new' ? null : editing}
          onClose={() => setEditing(null)}
          onSaved={() => {
            setEditing(null)
            q.reload()
          }}
        />
      ) : null}

      <ConfirmDialog
        open={deleting !== null}
        onOpenChange={(o) => !o && setDeleting(null)}
        title={t('devices.deleteTitle')}
        body={t('devices.deleteBody', { name: deleting?.name ?? '' })}
        confirmLabel={t('ui.delete')}
        destructive
        onConfirm={doDelete}
      />
    </section>
  )
}

function DeviceFormModal({
  existing,
  onClose,
  onSaved,
}: {
  existing: Device | null
  onClose: () => void
  onSaved: () => void
}) {
  const t = useT()
  const { toast } = useToast()
  const [form, setForm] = useState<DeviceWrite>({
    name: existing?.name ?? '',
    ip: existing?.ip ?? '',
    type: existing?.type ?? 'ap',
    location: existing?.location ?? '',
    notes: existing?.notes ?? '',
    enabled: existing?.enabled ?? true,
    snmp_community: '',
  })
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  function set<K extends keyof DeviceWrite>(k: K, v: DeviceWrite[K]) {
    setForm((prev) => ({ ...prev, [k]: v }))
  }

  async function submit() {
    setBusy(true)
    setError(null)
    try {
      const body: DeviceWrite = { ...form, snmp_community: form.snmp_community || null }
      if (existing) await updateDevice(existing.id, body)
      else await createDevice(body)
      toast(t('devices.saved'), 'ok')
      onSaved()
    } catch (err) {
      setError(err instanceof ApiError ? err.message : t('common.error.body'))
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal
      open
      onOpenChange={busy ? () => {} : (o) => !o && onClose()}
      title={existing ? t('devices.editTitle') : t('devices.createTitle')}
    >
      <form
        onSubmit={(e) => {
          e.preventDefault()
          void submit()
        }}
        className="space-y-4"
      >
        <div className="grid gap-3 sm:grid-cols-2">
          <Field label={t('devices.name')} htmlFor="dev-name">
            <TextInput
              id="dev-name"
              value={form.name}
              onChange={(e) => set('name', e.target.value)}
              required
            />
          </Field>
          <Field label={t('devices.ip')} htmlFor="dev-ip">
            <TextInput
              id="dev-ip"
              value={form.ip}
              onChange={(e) => set('ip', e.target.value)}
              dir="ltr"
              required
            />
          </Field>
          <Field label={t('devices.deviceType')} htmlFor="dev-type">
            <Select
              id="dev-type"
              value={form.type}
              onChange={(e) => set('type', e.target.value as DeviceType)}
            >
              {TYPES.map((ty) => (
                <option key={ty} value={ty}>
                  {t(`devices.type.${ty}`)}
                </option>
              ))}
            </Select>
          </Field>
          <Field label={t('devices.location')} htmlFor="dev-loc">
            <TextInput
              id="dev-loc"
              value={form.location}
              onChange={(e) => set('location', e.target.value)}
            />
          </Field>
        </div>
        <Field label={t('devices.snmp')} hint={t('devices.snmpHint')} htmlFor="dev-snmp">
          <TextInput
            id="dev-snmp"
            value={form.snmp_community ?? ''}
            onChange={(e) => set('snmp_community', e.target.value)}
            dir="ltr"
            autoComplete="off"
          />
        </Field>
        <Checkbox
          label={t('devices.enabled')}
          checked={form.enabled ?? true}
          onChange={(e) => set('enabled', e.target.checked)}
        />
        {error ? <p className="text-sm text-danger">{error}</p> : null}
        <div className="flex justify-end gap-2">
          <Button variant="ghost" disabled={busy} onClick={onClose}>
            {t('ui.cancel')}
          </Button>
          <Button type="submit" disabled={busy}>
            {busy ? t('ui.working') : t('ui.save')}
          </Button>
        </div>
      </form>
    </Modal>
  )
}
