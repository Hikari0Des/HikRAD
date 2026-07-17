import { useEffect, useState } from 'react'

import { useT, type TFunction } from '@hikrad/shared'

import { applyService, planService } from '../../api/nas'
import { listPools } from '../../api/pools'
import { ApiError } from '../../api/client'
import type {
  AutoSetupPreview,
  Nas,
  NasService,
  NasType,
  Pool,
  ServiceApplyResult,
} from '../../api/types'
import { Button } from '../../components/Button'
import { Modal } from '../../components/Modal'
import { useToast } from '../../components/Toast'

/**
 * Create-or-edit ONE Hotspot/PPPoE server (v2 phase 2, FR-67.3/67.4): the
 * same hash-gated preview/apply pattern as auto-setup, scoped to a single
 * server instance. Conflicts here are abort-only (no keep/update choice) —
 * "another service already has this identity" has no safe update meaning.
 */
export function ServiceProvisionModal({
  nas,
  editing,
  open,
  onOpenChange,
  onSaved,
}: {
  nas: Nas
  /** undefined = create a new server; a NasService = edit it (must be system-managed). */
  editing: NasService | undefined
  open: boolean
  onOpenChange: (o: boolean) => void
  onSaved: () => void
}) {
  const t = useT()
  const { toast } = useToast()
  const [pools, setPools] = useState<Pool[]>([])
  const [kind, setKind] = useState<NasType>(editing?.service ?? 'hotspot')
  const [label, setLabel] = useState(editing?.label ?? '')
  const [rosServerName, setRosServerName] = useState(editing?.ros_server_name ?? '')
  const [iface, setIface] = useState(editing?.interface_note ?? '')
  const [poolId, setPoolId] = useState<string>(editing?.ip_pool_id ?? '')
  const [preview, setPreview] = useState<AutoSetupPreview | null>(null)
  const [result, setResult] = useState<ServiceApplyResult | null>(null)
  const [loading, setLoading] = useState(false)
  const [applying, setApplying] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    if (!open) return
    listPools()
      .then((r) => setPools(r.items))
      .catch(() => setPools([]))
    setKind(editing?.service ?? 'hotspot')
    setLabel(editing?.label ?? '')
    setRosServerName(editing?.ros_server_name ?? '')
    setIface(editing?.interface_note ?? '')
    setPoolId(editing?.ip_pool_id ?? '')
    setPreview(null)
    setResult(null)
    setError(null)
  }, [open, editing])

  function body() {
    return {
      service_id: editing?.id,
      service: kind,
      label,
      ros_server_name: rosServerName,
      interface: iface,
      ip_pool_id: poolId || null,
    }
  }

  async function runPlan() {
    setLoading(true)
    setError(null)
    setResult(null)
    try {
      const res = await planService(nas.id, body())
      setPreview(res)
    } catch (err) {
      setError(errMessage(err, t))
    } finally {
      setLoading(false)
    }
  }

  async function runApply() {
    if (!preview) return
    setApplying(true)
    try {
      const res = await applyService(nas.id, { ...body(), preview_hash: preview.preview_hash })
      setResult(res)
      if (res.all_ok) {
        toast(t('nas.serverMgmt.applyOk'), 'ok')
        onSaved()
      } else {
        toast(t('nas.autoSetup.applyPartial'), 'danger')
      }
    } catch (err) {
      toast(errMessage(err, t), 'danger')
    } finally {
      setApplying(false)
    }
  }

  const valid = rosServerName.trim() !== '' && iface.trim() !== ''

  return (
    <Modal
      open={open}
      onOpenChange={applying ? () => {} : onOpenChange}
      size="lg"
      title={editing ? t('nas.serverMgmt.editTitle') : t('nas.serverMgmt.createTitle')}
    >
      <div className="space-y-4">
        {!preview && !result ? (
          <>
            <div className="grid gap-3 sm:grid-cols-2">
              <label className="text-xs">
                <span className="mb-1 block text-ink-muted">{t('nas.serviceKind')}</span>
                <select
                  className="w-full rounded border border-surface-sunken bg-surface px-2 py-1 text-sm"
                  value={kind}
                  disabled={!!editing}
                  onChange={(e) => setKind(e.target.value as NasType)}
                >
                  <option value="pppoe">{t('nas.typeName.pppoe')}</option>
                  <option value="hotspot">{t('nas.typeName.hotspot')}</option>
                </select>
              </label>
              <label className="text-xs">
                <span className="mb-1 block text-ink-muted">{t('nas.serviceLabel')}</span>
                <input
                  className="w-full rounded border border-surface-sunken bg-surface px-2 py-1 text-sm"
                  value={label}
                  onChange={(e) => setLabel(e.target.value)}
                  placeholder={t('nas.serviceLabelPlaceholder')}
                />
              </label>
              <label className="text-xs">
                <span className="mb-1 block text-ink-muted">{t('nas.rosServerName')}</span>
                <input
                  dir="ltr"
                  className="w-full rounded border border-surface-sunken bg-surface px-2 py-1 text-sm"
                  value={rosServerName}
                  onChange={(e) => setRosServerName(e.target.value)}
                  placeholder={t('nas.rosServerNamePlaceholder')}
                />
              </label>
              <label className="text-xs">
                <span className="mb-1 block text-ink-muted">{t('nas.serverMgmt.interface')}</span>
                <input
                  dir="ltr"
                  className="w-full rounded border border-surface-sunken bg-surface px-2 py-1 text-sm"
                  value={iface}
                  onChange={(e) => setIface(e.target.value)}
                />
              </label>
              <label className="text-xs sm:col-span-2">
                <span className="mb-1 block text-ink-muted">{t('nas.serverMgmt.pool')}</span>
                <select
                  className="w-full rounded border border-surface-sunken bg-surface px-2 py-1 text-sm"
                  value={poolId}
                  onChange={(e) => setPoolId(e.target.value)}
                >
                  <option value="">{t('ui.none')}</option>
                  {pools.map((p) => (
                    <option key={p.id} value={p.id}>
                      {p.name}
                    </option>
                  ))}
                </select>
              </label>
            </div>
            {error ? <p className="text-sm text-danger">{error}</p> : null}
            <Button disabled={!valid || loading} onClick={() => void runPlan()}>
              {loading ? t('ui.working') : t('nas.serverMgmt.planButton')}
            </Button>
          </>
        ) : null}

        {preview && !result ? (
          <div className="space-y-3">
            {preview.conflicts.length > 0 ? (
              <div className="rounded-md bg-danger/10 p-3 text-sm">
                <p className="mb-2 font-medium text-danger">{t('nas.autoSetup.conflictsTitle')}</p>
                <ul className="space-y-2">
                  {preview.conflicts.map((c, i) => (
                    <li key={i}>
                      <code dir="ltr" className="block text-xs text-ink-muted">
                        {c.path}
                      </code>
                      <p>{c.reason}</p>
                    </li>
                  ))}
                </ul>
              </div>
            ) : (
              <ul className="space-y-2">
                {preview.items.map((it, i) => (
                  <li key={i} className="rounded-md border border-surface-sunken p-2 text-sm">
                    <span className="me-2 rounded bg-surface-sunken px-1.5 py-0.5 text-xs">
                      {t(`nas.autoSetup.action.${it.action}`)}
                    </span>
                    <code dir="ltr" className="text-xs">
                      {it.path}
                    </code>
                    <pre
                      dir="ltr"
                      className="mt-1 overflow-x-auto rounded bg-ink/90 p-2 text-xs text-ink-inverse"
                    >
                      {it.command}
                    </pre>
                  </li>
                ))}
              </ul>
            )}
            <div className="flex justify-between gap-2 pt-2">
              <Button variant="ghost" size="sm" onClick={() => setPreview(null)}>
                {t('ui.back')}
              </Button>
              {preview.conflicts.length === 0 ? (
                <Button disabled={applying} onClick={() => void runApply()}>
                  {applying ? t('ui.working') : t('nas.serverMgmt.applyButton')}
                </Button>
              ) : null}
            </div>
          </div>
        ) : null}

        {result ? (
          <div className="space-y-2">
            <p className={`text-sm font-medium ${result.all_ok ? 'text-ok' : 'text-danger'}`}>
              {result.all_ok ? t('nas.serverMgmt.applyOk') : t('nas.autoSetup.applyPartial')}
            </p>
            <ul className="space-y-1 text-sm">
              {result.results.map((r, i) => (
                <li key={i} className="flex items-center justify-between gap-2">
                  <code dir="ltr" className="text-xs">
                    {r.path}
                  </code>
                  <span className={r.ok ? 'text-ok' : 'text-danger'}>
                    {r.ok ? t('ui.ok') : (r.error ?? t('common.error.title'))}
                  </span>
                </li>
              ))}
            </ul>
          </div>
        ) : null}
      </div>
    </Modal>
  )
}

function errMessage(err: unknown, t: TFunction): string {
  if (err instanceof ApiError) {
    if (err.code === 'router_unreachable') return t('nas.autoSetup.errUnreachable')
    if (err.code === 'no_api_credentials') return t('nas.autoSetup.noCreds')
    if (err.code === 'preview_stale') return t('nas.autoSetup.errStale')
    if (err.code === 'not_adopted') return t('nas.serverMgmt.notAdopted')
    return err.message
  }
  return err instanceof Error ? err.message : t('common.error.body')
}
