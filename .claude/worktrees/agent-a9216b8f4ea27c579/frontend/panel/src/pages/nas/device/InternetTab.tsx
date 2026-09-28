import { useEffect, useState } from 'react'

import { ErrorState, LoadingState, Ltr, useT } from '@hikrad/shared'

import { getInterfacesTab } from '../../../api/nasDevice'
import { adoptWanUplinks, applyWanUplink, listWanUplinks, planWanUplink } from '../../../api/nasWan'
import type {
  AutoSetupApplyResultItem,
  AutoSetupPreview,
  WanMode,
  WanUplink,
  WizardWanInput,
} from '../../../api/types'
import { useAuth } from '../../../auth/AuthContext'
import { PERM_NAS_EDIT } from '../../../auth/permissions'
import { Button } from '../../../components/Button'
import { Modal } from '../../../components/Modal'
import { OwnershipBadge } from '../../../components/OwnershipBadge'
import { Pill } from '../../../components/Pill'
import { PlanApplyResults, PlanPreview } from '../../../components/PlanPreview'
import { Select, TextInput } from '../../../components/form'
import { FormField } from '../../../components/patterns'
import { useToast } from '../../../components/Toast'
import { useAsync } from '../../../hooks/useAsync'
import { routerErrorMessage } from '../../../lib/routerErrors'

/**
 * FR-145 Internet tab: the router's WAN uplinks, with the phase's
 * "ours editable, pre-existing shown read-only" rule made literal — a row's
 * `management_mode` decides whether it gets an Edit control at all. A
 * `router` row is a reference HikRAD reports and never writes back; adopting
 * imports such rows explicitly, and even then writes nothing to the router.
 *
 * Unlike the v4-6 tabs this is a LIVE read (no device_config_cache envelope),
 * so it renders its own loading/error chrome rather than DeviceTabShell.
 */
export function InternetTab({ nasId }: { nasId: string }) {
  const t = useT()
  const { can } = useAuth()
  const { toast } = useToast()
  const { data, error, loading, reload } = useAsync(() => listWanUplinks(nasId), [nasId])
  // Gate item 10: the interface dropdown is fed by discovery, never typed blind.
  const ifaces = useAsync(() => getInterfacesTab(nasId), [nasId])

  const [formOpen, setFormOpen] = useState(false)
  const [editing, setEditing] = useState<WanUplink | null>(null)
  const [adoptOpen, setAdoptOpen] = useState(false)
  const [adopting, setAdopting] = useState(false)

  const canEdit = can(PERM_NAS_EDIT)
  const items = data?.items ?? []
  const interfaceNames = (ifaces.data?.data?.interfaces ?? []).map((i) => i.name)

  async function runAdopt() {
    setAdopting(true)
    try {
      const res = await adoptWanUplinks(nasId)
      toast(t('nas.internet.adoptOk', { count: res.adopted, total: res.total }), 'ok')
      setAdoptOpen(false)
      reload()
    } catch (err) {
      toast(routerErrorMessage(err, t), 'danger')
    } finally {
      setAdopting(false)
    }
  }

  if (error) return <ErrorState onRetry={reload} />
  if (loading) return <LoadingState />

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <p className="text-xs text-ink-muted">{t('nas.internet.hint')}</p>
        {canEdit ? (
          <div className="flex gap-2">
            <Button size="sm" variant="secondary" onClick={() => setAdoptOpen(true)}>
              {t('nas.internet.adoptButton')}
            </Button>
            <Button
              size="sm"
              onClick={() => {
                setEditing(null)
                setFormOpen(true)
              }}
            >
              {t('nas.internet.addUplink')}
            </Button>
          </div>
        ) : null}
      </div>

      {items.length === 0 ? (
        <p className="rounded-md border border-dashed border-border bg-surface-raised p-6 text-center text-sm text-ink-muted">
          {t('nas.internet.empty')}
        </p>
      ) : (
        <ul className="space-y-2">
          {items.map((u) => (
            <li key={u.interface} className="rounded-md border border-border bg-surface-raised p-3">
              <div className="flex flex-wrap items-center justify-between gap-2">
                <div className="flex items-center gap-2">
                  <span className="font-medium">
                    <Ltr>{u.interface}</Ltr>
                  </span>
                  <Pill tone="muted">{t(`nas.internet.mode.${u.mode}`)}</Pill>
                  <OwnershipBadge ownership={u.management_mode} />
                </div>
                {/* The read-only rule, enforced in the UI: a router-owned
                    uplink gets no edit control at all. */}
                {canEdit && u.management_mode === 'system' ? (
                  <Button
                    size="sm"
                    variant="ghost"
                    onClick={() => {
                      setEditing(u)
                      setFormOpen(true)
                    }}
                  >
                    {t('ui.edit')}
                  </Button>
                ) : (
                  <span className="text-xs text-ink-muted">{t('nas.internet.readOnly')}</span>
                )}
              </div>
              <dl className="mt-2 grid grid-cols-2 gap-x-4 gap-y-1 text-xs sm:grid-cols-4">
                <Detail label={t('nas.internet.address')} value={u.address} mono />
                <Detail label={t('nas.internet.gateway')} value={u.gateway} mono />
                <Detail label={t('nas.internet.status')} value={u.status ?? ''} mono />
                <Detail label={t('nas.internet.comment')} value={u.comment ?? ''} />
              </dl>
            </li>
          ))}
        </ul>
      )}

      <WanUplinkModal
        nasId={nasId}
        open={formOpen}
        editing={editing}
        interfaceNames={interfaceNames}
        onOpenChange={setFormOpen}
        onApplied={() => {
          setFormOpen(false)
          reload()
        }}
      />

      <Modal
        open={adoptOpen}
        onOpenChange={(o) => !o && !adopting && setAdoptOpen(false)}
        title={t('nas.internet.adoptTitle')}
      >
        <p className="text-sm text-ink-muted">
          {t('nas.internet.adoptBody', { count: items.length })}
        </p>
        <div className="mt-6 flex justify-end gap-2">
          <Button variant="ghost" disabled={adopting} onClick={() => setAdoptOpen(false)}>
            {t('ui.cancel')}
          </Button>
          <Button disabled={adopting} onClick={() => void runAdopt()}>
            {adopting ? t('ui.working') : t('nas.internet.adoptConfirm')}
          </Button>
        </div>
      </Modal>
    </div>
  )
}

function Detail({ label, value, mono }: { label: string; value: string; mono?: boolean }) {
  return (
    <div>
      <dt className="text-ink-muted">{label}</dt>
      <dd>{mono ? <Ltr>{value || '—'}</Ltr> : value || '—'}</dd>
    </div>
  )
}

/**
 * Create-or-edit one uplink on the plan → preview → apply pipeline. Conflicts
 * here are abort-only (PlanPreview mode="abort"): the vendor layer emits no
 * resolvable conflict for a WAN uplink, so there is no honest keep/update
 * choice to offer — only cancel.
 */
function WanUplinkModal({
  nasId,
  open,
  editing,
  interfaceNames,
  onOpenChange,
  onApplied,
}: {
  nasId: string
  open: boolean
  editing: WanUplink | null
  interfaceNames: string[]
  onOpenChange: (o: boolean) => void
  onApplied: () => void
}) {
  const t = useT()
  const { toast } = useToast()
  const [iface, setIface] = useState('')
  const [label, setLabel] = useState('')
  const [mode, setMode] = useState<WanMode>('dhcp')
  const [address, setAddress] = useState('')
  const [gateway, setGateway] = useState('')
  const [pppoeUser, setPppoeUser] = useState('')
  const [pppoePassword, setPppoePassword] = useState('')
  const [preview, setPreview] = useState<AutoSetupPreview | null>(null)
  const [results, setResults] = useState<{
    items: AutoSetupApplyResultItem[]
    allOk: boolean
  } | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    if (!open) return
    setIface(editing?.interface ?? '')
    setLabel(editing?.comment ?? '')
    setMode(editing?.mode ?? 'dhcp')
    setAddress(editing?.address ?? '')
    setGateway(editing?.gateway ?? '')
    setPppoeUser('')
    setPppoePassword('')
    setPreview(null)
    setResults(null)
    setError(null)
  }, [open, editing])

  function body(): WizardWanInput {
    return {
      interface: iface,
      label,
      mode,
      address: mode === 'static' ? address : '',
      gateway: mode === 'static' ? gateway : '',
      pppoe_user: mode === 'pppoe' ? pppoeUser : '',
      pppoe_password: mode === 'pppoe' ? pppoePassword : '',
    }
  }

  async function runPlan() {
    setBusy(true)
    setError(null)
    try {
      setPreview(await planWanUplink(nasId, body()))
    } catch (err) {
      setError(routerErrorMessage(err, t))
    } finally {
      setBusy(false)
    }
  }

  async function runApply() {
    if (!preview) return
    setBusy(true)
    try {
      const res = await applyWanUplink(nasId, { ...body(), preview_hash: preview.preview_hash })
      setResults({ items: res.results, allOk: res.all_ok })
      if (res.all_ok) {
        toast(t('nas.internet.applyOk'), 'ok')
        onApplied()
      } else {
        toast(t('nas.autoSetup.applyPartial'), 'danger')
      }
    } catch (err) {
      toast(routerErrorMessage(err, t), 'danger')
    } finally {
      setBusy(false)
    }
  }

  const valid = iface.trim() !== '' && (mode !== 'static' || address.trim() !== '')

  return (
    <Modal
      open={open}
      onOpenChange={busy ? () => {} : onOpenChange}
      size="lg"
      title={editing ? t('nas.internet.editTitle') : t('nas.internet.createTitle')}
    >
      <div className="space-y-4">
        {!preview && !results ? (
          <>
            <div className="grid gap-3 sm:grid-cols-2">
              <FormField
                label={t('nas.internet.interface')}
                hint={interfaceNames.length > 0 ? t('nas.internet.fromRouterHint') : undefined}
              >
                {(id) => (
                  <Select
                    id={id}
                    value={iface}
                    disabled={!!editing}
                    onChange={(e) => setIface(e.target.value)}
                  >
                    <option value="">{t('ui.none')}</option>
                    {interfaceNames.map((n) => (
                      <option key={n} value={n}>
                        {n}
                      </option>
                    ))}
                    {/* Escape hatch: keep an already-chosen name selectable even
                        when discovery hasn't reported it (unreachable router). */}
                    {iface && !interfaceNames.includes(iface) ? (
                      <option value={iface}>{iface}</option>
                    ) : null}
                  </Select>
                )}
              </FormField>
              <FormField label={t('nas.internet.label')}>
                {(id) => (
                  <TextInput id={id} value={label} onChange={(e) => setLabel(e.target.value)} />
                )}
              </FormField>
              <FormField label={t('nas.internet.modeLabel')}>
                {(id) => (
                  <Select id={id} value={mode} onChange={(e) => setMode(e.target.value as WanMode)}>
                    <option value="dhcp">{t('nas.internet.mode.dhcp')}</option>
                    <option value="static">{t('nas.internet.mode.static')}</option>
                    <option value="pppoe">{t('nas.internet.mode.pppoe')}</option>
                  </Select>
                )}
              </FormField>
              {mode === 'static' ? (
                <>
                  <FormField label={t('nas.internet.address')}>
                    {(id) => (
                      <TextInput
                        id={id}
                        dir="ltr"
                        value={address}
                        onChange={(e) => setAddress(e.target.value)}
                      />
                    )}
                  </FormField>
                  <FormField label={t('nas.internet.gateway')}>
                    {(id) => (
                      <TextInput
                        id={id}
                        dir="ltr"
                        value={gateway}
                        onChange={(e) => setGateway(e.target.value)}
                      />
                    )}
                  </FormField>
                </>
              ) : null}
              {mode === 'pppoe' ? (
                <>
                  <FormField label={t('nas.internet.pppoeUser')}>
                    {(id) => (
                      <TextInput
                        id={id}
                        dir="ltr"
                        value={pppoeUser}
                        onChange={(e) => setPppoeUser(e.target.value)}
                      />
                    )}
                  </FormField>
                  <FormField label={t('nas.internet.pppoePassword')}>
                    {(id) => (
                      <TextInput
                        id={id}
                        dir="ltr"
                        type="password"
                        value={pppoePassword}
                        onChange={(e) => setPppoePassword(e.target.value)}
                      />
                    )}
                  </FormField>
                </>
              ) : null}
            </div>
            {error ? <p className="text-sm text-danger">{error}</p> : null}
            <Button disabled={!valid || busy} onClick={() => void runPlan()}>
              {busy ? t('ui.working') : t('nas.plan.previewButton')}
            </Button>
          </>
        ) : null}

        {preview && !results ? (
          <div className="space-y-3">
            <PlanPreview preview={preview} mode="abort" />
            <div className="flex justify-between gap-2 pt-2">
              <Button variant="ghost" size="sm" disabled={busy} onClick={() => setPreview(null)}>
                {t('ui.back')}
              </Button>
              {preview.conflicts.length === 0 && preview.items.length > 0 ? (
                <Button disabled={busy} onClick={() => void runApply()}>
                  {busy ? t('ui.working') : t('nas.plan.applyButton')}
                </Button>
              ) : null}
            </div>
          </div>
        ) : null}

        {results ? <PlanApplyResults results={results.items} allOk={results.allOk} /> : null}
      </div>
    </Modal>
  )
}
