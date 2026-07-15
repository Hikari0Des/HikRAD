import { useRef, useState } from 'react'

import { useT, type TFunction } from '@hikrad/shared'

import {
  dryRunImport,
  executeImport,
  fileToBase64,
  getImportBatch,
  mapImportColumns,
  uploadImportFile,
  type BatchStatus,
  type DryRunResult,
  type UploadResult,
} from '../../api/importer'
import { triggerDownload } from '../../api/billing'
import { ApiError } from '../../api/client'
import { Button } from '../../components/Button'
import { Select } from '../../components/form'
import { PageHeader } from '../../components/PageHeader'
import { useToast } from '../../components/Toast'

const HIKRAD_FIELDS = [
  'username',
  'password',
  'name',
  'phone',
  'address',
  'profile',
  'expires_at',
] as const

type Step = 'upload' | 'map' | 'dryrun' | 'execute' | 'summary'

/**
 * SAS4/CSV import wizard (FR-6, task 3): upload → map → dry-run → execute →
 * summary. Every failure surfaces a next step ("never dead-end") — a bad
 * upload re-shows the picker with the reason, a failed map re-shows the
 * mapping form with field errors, etc.
 */
export function ImportWizardPage() {
  const t = useT()
  const { toast } = useToast()
  const [step, setStep] = useState<Step>('upload')
  const [upload, setUpload] = useState<UploadResult | null>(null)
  const [columnMap, setColumnMap] = useState<Record<string, string>>({})
  const [dryRun, setDryRun] = useState<DryRunResult | null>(null)
  const [progress, setProgress] = useState<BatchStatus | null>(null)
  const [busy, setBusy] = useState(false)
  const [uploadError, setUploadError] = useState<string | null>(null)
  const fileRef = useRef<HTMLInputElement>(null)

  async function onFileChosen(e: React.ChangeEvent<HTMLInputElement>) {
    const file = e.target.files?.[0]
    if (!file) return
    setBusy(true)
    setUploadError(null)
    try {
      const base64 = await fileToBase64(file)
      const res = await uploadImportFile(file.name, base64, 'sas4')
      setUpload(res)
      if (res.column_map) setColumnMap(res.column_map)
      setStep('map')
    } catch (err) {
      setUploadError(errMessage(err, t))
    } finally {
      setBusy(false)
      if (fileRef.current) fileRef.current.value = ''
    }
  }

  function applyPreset() {
    if (!upload) return
    const byLower = new Map(upload.header.map((h) => [h.toLowerCase(), h]))
    const preset: Record<string, string> = {
      username: 'username',
      password: 'password',
      name: 'fullname',
      phone: 'mobile',
      address: 'address',
      profile: 'package',
      expires_at: 'expiredate',
    }
    const next: Record<string, string> = {}
    for (const [field, guess] of Object.entries(preset)) {
      const match = byLower.get(guess)
      if (match) next[field] = match
    }
    setColumnMap(next)
  }

  async function saveMap() {
    if (!upload) return
    setBusy(true)
    try {
      await mapImportColumns(upload.batch_id, columnMap)
      const res = await dryRunImport(upload.batch_id)
      setDryRun(res)
      setStep('dryrun')
    } catch (err) {
      toast(errMessage(err, t), 'danger')
    } finally {
      setBusy(false)
    }
  }

  function downloadDryRun() {
    if (!dryRun) return
    const header = 'row,action,errors,warnings\n'
    const rows = dryRun.rows
      .map((r) => `${r.row},${r.action},"${r.errors.join('; ')}","${r.warnings.join('; ')}"`)
      .join('\n')
    triggerDownload(header + rows, `import-dry-run-${upload?.batch_id}.csv`, 'text/csv')
  }

  async function runExecute() {
    if (!upload) return
    setBusy(true)
    setStep('execute')
    try {
      await executeImport(upload.batch_id)
      await poll(upload.batch_id)
    } catch (err) {
      toast(errMessage(err, t), 'danger')
      setStep('dryrun')
    } finally {
      setBusy(false)
    }
  }

  async function poll(batchId: string) {
    for (;;) {
      const status = await getImportBatch(batchId)
      setProgress(status)
      if (status.status === 'completed' || status.progress?.status === 'completed') {
        setStep('summary')
        return
      }
      await new Promise((r) => setTimeout(r, 1000))
    }
  }

  function reset() {
    setStep('upload')
    setUpload(null)
    setColumnMap({})
    setDryRun(null)
    setProgress(null)
    setUploadError(null)
  }

  return (
    <section>
      <PageHeader title={t('import.title')} subtitle={t('import.subtitle')} />
      <ImportSteps step={step} />

      {step === 'upload' && (
        <div className="max-w-md space-y-3">
          <p className="text-sm text-ink-muted">{t('import.upload.body')}</p>
          {uploadError ? (
            <div className="rounded-md bg-danger/10 p-3 text-sm text-danger">{uploadError}</div>
          ) : null}
          <input
            ref={fileRef}
            type="file"
            accept=".csv,text/csv"
            disabled={busy}
            onChange={(e) => void onFileChosen(e)}
          />
          {busy ? <p className="text-sm text-ink-muted">{t('common.loading')}</p> : null}
        </div>
      )}

      {step === 'map' && upload && (
        <div className="max-w-xl space-y-4">
          <p className="text-sm text-ink-muted">
            {t('import.map.detected', {
              encoding: upload.encoding,
              header: upload.header.join(', '),
            })}
          </p>
          <Button variant="secondary" size="sm" onClick={applyPreset}>
            {t('import.map.sas4Preset')}
          </Button>
          <div className="grid gap-3 sm:grid-cols-2">
            {HIKRAD_FIELDS.map((field) => (
              <label key={field} className="text-xs">
                <span className="mb-1 block text-ink-muted">
                  {t(`import.field.${field}`)}
                  {field === 'username' ? ' *' : ''}
                </span>
                <Select
                  value={columnMap[field] ?? ''}
                  onChange={(e) => setColumnMap((m) => ({ ...m, [field]: e.target.value }))}
                >
                  <option value="">{t('import.map.notMapped')}</option>
                  {upload.header.map((h) => (
                    <option key={h} value={h}>
                      {h}
                    </option>
                  ))}
                </Select>
              </label>
            ))}
          </div>
          <Button disabled={busy || !columnMap.username} onClick={() => void saveMap()}>
            {busy ? t('ui.working') : t('import.map.dryRun')}
          </Button>
        </div>
      )}

      {step === 'dryrun' && dryRun && (
        <div className="space-y-4">
          <div className="flex flex-wrap items-center gap-3">
            <div className="rounded-md bg-ok/10 px-3 py-1.5 text-sm text-ok">
              {t('import.dryRun.willCreate', { n: dryRun.will_create })}
            </div>
            <div className="rounded-md bg-warn/10 px-3 py-1.5 text-sm text-warn">
              {t('import.dryRun.willSkip', { n: dryRun.will_skip })}
            </div>
            <Button variant="ghost" size="sm" onClick={downloadDryRun}>
              {t('import.dryRun.download')}
            </Button>
          </div>
          <div className="max-h-96 overflow-auto rounded-md border border-surface-sunken">
            <table className="w-full text-sm">
              <thead className="sticky top-0 bg-surface-sunken/60 text-start text-xs text-ink-muted">
                <tr>
                  <th className="px-3 py-2 text-start font-medium">{t('import.dryRun.row')}</th>
                  <th className="px-3 py-2 text-start font-medium">{t('import.dryRun.action')}</th>
                  <th className="px-3 py-2 text-start font-medium">{t('import.dryRun.issues')}</th>
                </tr>
              </thead>
              <tbody>
                {dryRun.rows.map((r) => (
                  <tr key={r.row} className="border-t border-surface-sunken/60">
                    <td className="px-3 py-2">{r.row}</td>
                    <td className="px-3 py-2">
                      <span className={r.action === 'create' ? 'text-ok' : 'text-warn'}>
                        {t(`import.dryRun.action.${r.action}`)}
                      </span>
                    </td>
                    <td className="px-3 py-2">
                      {r.errors.map((e, i) => (
                        <p key={`e${i}`} className="text-danger">
                          {e}
                        </p>
                      ))}
                      {r.warnings.map((w, i) => (
                        <p key={`w${i}`} className="text-warn">
                          {w}
                        </p>
                      ))}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          <div className="flex justify-between gap-2">
            <Button variant="ghost" onClick={() => setStep('map')}>
              {t('ui.back')}
            </Button>
            <Button disabled={busy || dryRun.will_create === 0} onClick={() => void runExecute()}>
              {t('import.execute')}
            </Button>
          </div>
        </div>
      )}

      {step === 'execute' && (
        <div className="max-w-md space-y-3">
          <p className="text-sm text-ink-muted">{t('import.executing')}</p>
          {progress?.progress ? (
            <div className="h-2 overflow-hidden rounded-full bg-surface-sunken">
              <div
                className="h-full bg-brand transition-all"
                style={{
                  width: `${progress.progress.total ? Math.round((progress.progress.done / progress.progress.total) * 100) : 0}%`,
                }}
              />
            </div>
          ) : null}
          {progress?.progress ? (
            <p className="text-xs text-ink-muted">
              {t('import.progress', {
                done: progress.progress.done,
                total: progress.progress.total,
              })}
            </p>
          ) : null}
        </div>
      )}

      {step === 'summary' && progress && (
        <div className="max-w-md space-y-4">
          <div className="rounded-md border border-surface-sunken bg-surface-raised p-4">
            <p className="text-sm">
              {t('import.summary.body', {
                succeeded: progress.progress?.created ?? 0,
                failed: progress.progress?.failed ?? 0,
              })}
            </p>
          </div>
          <Button onClick={reset}>{t('import.newImport')}</Button>
        </div>
      )}
    </section>
  )
}

const STEP_ORDER: Step[] = ['upload', 'map', 'dryrun', 'execute', 'summary']

function ImportSteps({ step }: { step: Step }) {
  const t = useT()
  const idx = STEP_ORDER.indexOf(step)
  return (
    <ol className="mb-6 flex flex-wrap gap-2 text-xs">
      {STEP_ORDER.map((s, i) => (
        <li
          key={s}
          className={`rounded-full px-3 py-1 ${
            i === idx
              ? 'bg-brand text-ink-inverse'
              : i < idx
                ? 'bg-ok/15 text-ok'
                : 'bg-surface-sunken text-ink-muted'
          }`}
        >
          {i + 1}. {t(`import.step.${s}`)}
        </li>
      ))}
    </ol>
  )
}

function errMessage(err: unknown, t: TFunction): string {
  if (err instanceof ApiError) {
    if (err.fieldErrors.length > 0) return err.fieldErrors.map((f) => f.message).join('; ')
    return `${err.message} ${t('import.tryAgain')}`
  }
  return err instanceof Error ? err.message : t('common.error.body')
}
