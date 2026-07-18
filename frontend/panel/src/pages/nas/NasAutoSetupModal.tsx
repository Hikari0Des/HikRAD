import { useEffect, useState } from 'react'

import { useT, type TFunction } from '@hikrad/shared'

import { applyAutoSetup, nasConfig, previewAutoSetup } from '../../api/nas'
import { ApiError } from '../../api/client'
import type {
  AutoSetupApplyResult,
  AutoSetupPreview,
  AutoSetupResolution,
  AutoSetupValues,
  Nas,
  NasConfig,
} from '../../api/types'
import { rosMatrixValidated } from '../../lib/rosMatrix'
import { Button } from '../../components/Button'
import { Modal } from '../../components/Modal'
import { RadioGroup, RadioOption } from '../../components/form'
import { useToast } from '../../components/Toast'

/**
 * NAS API auto-setup (FR-56.2-56.4, task 2b; extended v2 phase 2 FR-65/66):
 * a "current config" read-only view, a values-form the operator can edit
 * before previewing, and per-conflict keep/update/abort resolution — preview
 * the diff, highlight conflicts in plain language, require an explicit
 * confirm before writing anything. Disabled (apply) for ROS versions without
 * a green matrix leg — preview stays available either way, and the
 * copy-paste path is one click away via the "Use copy-paste instead" link.
 */
export function NasAutoSetupModal({
  nas,
  open,
  onOpenChange,
  onUseSnippet,
}: {
  nas: Nas
  open: boolean
  onOpenChange: (o: boolean) => void
  onUseSnippet: () => void
}) {
  const t = useT()
  const { toast } = useToast()
  const [config, setConfig] = useState<NasConfig | null>(null)
  const [configLoading, setConfigLoading] = useState(false)
  const [values, setValues] = useState<AutoSetupValues>({})
  const [preview, setPreview] = useState<AutoSetupPreview | null>(null)
  const [resolutions, setResolutions] = useState<Record<string, AutoSetupResolution>>({})
  const [result, setResult] = useState<AutoSetupApplyResult | null>(null)
  const [loading, setLoading] = useState(false)
  const [applying, setApplying] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const rosOk = rosMatrixValidated(nas.ros_version)
  const hasCreds = nas.has_api_creds

  useEffect(() => {
    if (!open || !hasCreds) return
    setConfigLoading(true)
    nasConfig(nas.id)
      .then(setConfig)
      .catch(() => setConfig(null))
      .finally(() => setConfigLoading(false))
    // Values-form pre-fills from the NAS record; the operator edits before
    // the first preview (FR-66.1).
    setValues({ coa_port: nas.coa_port })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, hasCreds, nas.id])

  async function runPreview(withResolutions?: Record<string, AutoSetupResolution>) {
    setLoading(true)
    setError(null)
    setResult(null)
    try {
      const res = await previewAutoSetup(nas.id, {
        values: normalizedValues(values),
        resolutions: withResolutions ?? {},
      })
      setPreview(res)
      setResolutions(withResolutions ?? {})
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
      const res = await applyAutoSetup(nas.id, preview.preview_hash, {
        values: normalizedValues(values),
        resolutions,
      })
      setResult(res)
      if (res.all_ok) toast(t('nas.autoSetup.applyOk'), 'ok')
      else toast(t('nas.autoSetup.applyPartial'), 'danger')
    } catch (err) {
      toast(errMessage(err, t), 'danger')
    } finally {
      setApplying(false)
    }
  }

  function setResolution(key: string, choice: AutoSetupResolution | undefined) {
    setResolutions((prev) => {
      const next = { ...prev }
      if (choice) next[key] = choice
      else delete next[key]
      return next
    })
  }

  const unresolvedCount = preview ? preview.conflicts.filter((c) => !resolutions[c.key]).length : 0
  const canApply = !!preview && preview.conflicts.length === 0

  function close(o: boolean) {
    if (!o) {
      setPreview(null)
      setResult(null)
      setError(null)
      setResolutions({})
    }
    onOpenChange(o)
  }

  return (
    <Modal
      open={open}
      onOpenChange={applying ? () => {} : close}
      size="lg"
      title={t('nas.autoSetup.title', { name: nas.name })}
      description={t('nas.autoSetup.hint')}
    >
      {!hasCreds ? (
        <div className="rounded-md bg-warn/10 p-4 text-sm text-warn">
          {t('nas.autoSetup.noCreds')}
        </div>
      ) : (
        <>
          {!preview && !loading ? (
            <div className="space-y-4">
              <CurrentConfigPanel config={config} loading={configLoading} t={t} />
              <ValuesForm values={values} onChange={setValues} t={t} />
              <Button onClick={() => void runPreview()}>{t('nas.autoSetup.preview')}</Button>
            </div>
          ) : null}
          {loading ? <p className="text-sm text-ink-muted">{t('common.loading')}</p> : null}
          {error ? <p className="text-sm text-danger">{error}</p> : null}

          {preview && !result ? (
            <div className="space-y-4">
              {preview.conflicts.length > 0 ? (
                <div className="rounded-md bg-danger/10 p-3 text-sm">
                  <p className="mb-2 font-medium text-danger">
                    {t('nas.autoSetup.conflictsTitle')}
                  </p>
                  <ul className="space-y-3">
                    {preview.conflicts.map((c) => (
                      <li key={c.key} className="rounded border border-danger/20 bg-surface p-2">
                        <code dir="ltr" className="block text-xs text-ink-muted">
                          {c.path}
                        </code>
                        <p>{c.reason}</p>
                        {c.resolvable && c.update_command ? (
                          <pre
                            dir="ltr"
                            className="mt-1 overflow-x-auto rounded bg-ink/90 p-2 text-xs text-ink-inverse"
                          >
                            {c.update_command}
                          </pre>
                        ) : null}
                        <RadioGroup
                          className="mt-2 text-xs"
                          name={`resolution-${c.key}`}
                          value={resolutions[c.key] ?? 'abort'}
                          onValueChange={(v) =>
                            setResolution(
                              c.key,
                              v === 'abort' ? undefined : (v as AutoSetupResolution),
                            )
                          }
                        >
                          <RadioOption value="abort" label={t('nas.autoSetup.resolution.abort')} />
                          <RadioOption value="keep" label={t('nas.autoSetup.resolution.keep')} />
                          {c.resolvable ? (
                            <RadioOption
                              value="update"
                              label={t('nas.autoSetup.resolution.update')}
                            />
                          ) : null}
                        </RadioGroup>
                      </li>
                    ))}
                  </ul>
                  <p className="mt-2 text-xs text-ink-muted">{t('nas.autoSetup.conflictsBody')}</p>
                  <Button
                    size="sm"
                    className="mt-2"
                    disabled={
                      unresolvedCount === preview.conflicts.length &&
                      unresolvedCount > 0 &&
                      Object.keys(resolutions).length === 0
                    }
                    onClick={() => void runPreview(resolutions)}
                  >
                    {t('nas.autoSetup.applyResolutions')}
                  </Button>
                </div>
              ) : (
                <div>
                  <p className="mb-2 text-sm font-medium">
                    {t('nas.autoSetup.itemsTitle', { count: preview.items.length })}
                  </p>
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
                </div>
              )}

              {!rosOk ? (
                <div className="rounded-md bg-warn/10 p-3 text-sm text-warn">
                  {t('nas.autoSetup.rosNotValidated')}
                </div>
              ) : null}

              <div className="flex flex-wrap justify-between gap-2 pt-2">
                <div className="flex gap-2">
                  <Button variant="ghost" size="sm" onClick={() => void runPreview(resolutions)}>
                    {t('ui.refresh')}
                  </Button>
                  <Button variant="secondary" size="sm" onClick={onUseSnippet}>
                    {t('nas.autoSetup.useSnippet')}
                  </Button>
                </div>
                {canApply ? (
                  <Button disabled={!rosOk || applying} onClick={() => void runApply()}>
                    {applying ? t('ui.working') : t('nas.autoSetup.apply')}
                  </Button>
                ) : null}
              </div>
            </div>
          ) : null}

          {result ? (
            <div className="space-y-2">
              <p className={`text-sm font-medium ${result.all_ok ? 'text-ok' : 'text-danger'}`}>
                {result.all_ok ? t('nas.autoSetup.applyOk') : t('nas.autoSetup.applyPartial')}
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
              <p className="text-xs text-ink-muted">
                {result.seen.seen ? t('nas.testSeen', { when: '' }) : t('nas.testUnseen')}
              </p>
            </div>
          ) : null}
        </>
      )}
    </Modal>
  )
}

/** FR-65's read-only "current config" tab. */
function CurrentConfigPanel({
  config,
  loading,
  t,
}: {
  config: NasConfig | null
  loading: boolean
  t: TFunction
}) {
  if (loading) return <p className="text-xs text-ink-muted">{t('common.loading')}</p>
  if (!config) return null
  const hasAny = config.radius.length > 0 || config.hotspot_profiles.length > 0
  return (
    <details className="rounded-md border border-surface-sunken p-3 text-xs">
      <summary className="cursor-pointer select-none font-medium text-ink">
        {t('nas.autoSetup.currentConfigTitle')}
      </summary>
      <div className="mt-2 space-y-2">
        {!hasAny ? (
          <p className="text-ink-muted">{t('nas.autoSetup.currentEmpty')}</p>
        ) : (
          <>
            {config.radius.map((r, i) => (
              <div key={i}>
                <span className="text-ink-muted">{t('nas.autoSetup.currentRadius')}: </span>
                <code dir="ltr">
                  {r.address} ({r.service}) {r.comment}
                </code>
              </div>
            ))}
            <div>
              <span className="text-ink-muted">{t('nas.autoSetup.currentRadiusIncoming')}: </span>
              <code dir="ltr">
                accept={String(config.radius_incoming.accept)} port={config.radius_incoming.port}
              </code>
            </div>
            <div>
              <span className="text-ink-muted">{t('nas.autoSetup.currentPppAaa')}: </span>
              <code dir="ltr">
                use-radius={String(config.ppp_aaa.use_radius)} interim=
                {config.ppp_aaa.interim_update_secs}s
              </code>
            </div>
            {config.hotspot_profiles.map((p, i) => (
              <div key={i}>
                <span className="text-ink-muted">
                  {t('nas.autoSetup.currentHotspotProfiles')}:{' '}
                </span>
                <code dir="ltr">
                  {p.name} use-radius={String(p.use_radius)}
                </code>
              </div>
            ))}
            {config.walled_garden.length > 0 ? (
              <div>
                <span className="text-ink-muted">{t('nas.autoSetup.currentWalledGarden')}: </span>
                <code dir="ltr">{config.walled_garden.join(', ')}</code>
              </div>
            ) : null}
          </>
        )}
      </div>
    </details>
  )
}

/** FR-66.1 values-form: overrides that feed both preview/apply and the snippet endpoint. */
function ValuesForm({
  values,
  onChange,
  t,
}: {
  values: AutoSetupValues
  onChange: (v: AutoSetupValues) => void
  t: TFunction
}) {
  return (
    <div className="grid gap-3 sm:grid-cols-2">
      <label className="text-xs">
        <span className="mb-1 block text-ink-muted">{t('nas.autoSetup.valuesRadiusServer')}</span>
        <input
          dir="ltr"
          className="w-full rounded border border-surface-sunken bg-surface px-2 py-1 text-sm"
          value={values.radius_server ?? ''}
          onChange={(e) => onChange({ ...values, radius_server: e.target.value || undefined })}
        />
      </label>
      <label className="text-xs">
        <span className="mb-1 block text-ink-muted">{t('nas.autoSetup.valuesCoaPort')}</span>
        <input
          type="number"
          dir="ltr"
          className="w-full rounded border border-surface-sunken bg-surface px-2 py-1 text-sm"
          value={values.coa_port ?? ''}
          onChange={(e) =>
            onChange({ ...values, coa_port: e.target.value ? Number(e.target.value) : undefined })
          }
        />
      </label>
      <label className="text-xs">
        <span className="mb-1 block text-ink-muted">{t('nas.autoSetup.valuesInterimSecs')}</span>
        <input
          type="number"
          dir="ltr"
          className="w-full rounded border border-surface-sunken bg-surface px-2 py-1 text-sm"
          value={values.interim_secs ?? ''}
          onChange={(e) =>
            onChange({
              ...values,
              interim_secs: e.target.value ? Number(e.target.value) : undefined,
            })
          }
        />
      </label>
      <label className="text-xs sm:col-span-2">
        <span className="mb-1 block text-ink-muted">{t('nas.autoSetup.valuesWalledGarden')}</span>
        <input
          dir="ltr"
          className="w-full rounded border border-surface-sunken bg-surface px-2 py-1 text-sm"
          value={(values.walled_garden ?? []).join(', ')}
          onChange={(e) =>
            onChange({
              ...values,
              walled_garden: e.target.value
                ? e.target.value
                    .split(',')
                    .map((h) => h.trim())
                    .filter(Boolean)
                : undefined,
            })
          }
        />
      </label>
    </div>
  )
}

/** Drops empty/undefined fields so an untouched form sends `{}` — no overrides at all. */
function normalizedValues(v: AutoSetupValues): AutoSetupValues {
  const out: AutoSetupValues = {}
  if (v.radius_server) out.radius_server = v.radius_server
  if (v.src_address) out.src_address = v.src_address
  if (v.coa_port) out.coa_port = v.coa_port
  if (v.interim_secs) out.interim_secs = v.interim_secs
  if (v.walled_garden && v.walled_garden.length > 0) out.walled_garden = v.walled_garden
  return out
}

function errMessage(err: unknown, t: TFunction): string {
  if (err instanceof ApiError) {
    if (err.code === 'router_unreachable') return t('nas.autoSetup.errUnreachable')
    if (err.code === 'no_api_credentials') return t('nas.autoSetup.noCreds')
    if (err.code === 'preview_stale') return t('nas.autoSetup.errStale')
    return err.message
  }
  return err instanceof Error ? err.message : t('common.error.body')
}
