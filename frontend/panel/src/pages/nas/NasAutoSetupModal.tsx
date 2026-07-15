import { useState } from 'react'

import { useT, type TFunction } from '@hikrad/shared'

import { applyAutoSetup, previewAutoSetup } from '../../api/nas'
import { ApiError } from '../../api/client'
import type { AutoSetupApplyResult, AutoSetupPreview, Nas } from '../../api/types'
import { rosMatrixValidated } from '../../lib/rosMatrix'
import { Button } from '../../components/Button'
import { Modal } from '../../components/Modal'
import { useToast } from '../../components/Toast'

/**
 * NAS API auto-setup (FR-56.2-56.4, task 2b): preview the diff, highlight
 * conflicts in plain language, require an explicit confirm before writing
 * anything. Disabled (apply) for ROS versions without a green matrix leg —
 * preview stays available either way, and the copy-paste path is one click
 * away via the "Use copy-paste instead" link.
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
  const [preview, setPreview] = useState<AutoSetupPreview | null>(null)
  const [result, setResult] = useState<AutoSetupApplyResult | null>(null)
  const [loading, setLoading] = useState(false)
  const [applying, setApplying] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const rosOk = rosMatrixValidated(nas.ros_version)
  const hasCreds = nas.has_api_creds

  async function runPreview() {
    setLoading(true)
    setError(null)
    setResult(null)
    try {
      const res = await previewAutoSetup(nas.id)
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
      const res = await applyAutoSetup(nas.id, preview.preview_hash)
      setResult(res)
      if (res.all_ok) toast(t('nas.autoSetup.applyOk'), 'ok')
      else toast(t('nas.autoSetup.applyPartial'), 'danger')
    } catch (err) {
      toast(errMessage(err, t), 'danger')
    } finally {
      setApplying(false)
    }
  }

  function close(o: boolean) {
    if (!o) {
      setPreview(null)
      setResult(null)
      setError(null)
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
            <Button onClick={() => void runPreview()}>{t('nas.autoSetup.preview')}</Button>
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
                  <p className="mt-2 text-xs text-ink-muted">{t('nas.autoSetup.conflictsBody')}</p>
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
                  <Button variant="ghost" size="sm" onClick={() => void runPreview()}>
                    {t('ui.refresh')}
                  </Button>
                  <Button variant="secondary" size="sm" onClick={onUseSnippet}>
                    {t('nas.autoSetup.useSnippet')}
                  </Button>
                </div>
                {preview.conflicts.length === 0 ? (
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

function errMessage(err: unknown, t: TFunction): string {
  if (err instanceof ApiError) {
    if (err.code === 'router_unreachable') return t('nas.autoSetup.errUnreachable')
    if (err.code === 'no_api_credentials') return t('nas.autoSetup.noCreds')
    if (err.code === 'preview_stale') return t('nas.autoSetup.errStale')
    return err.message
  }
  return err instanceof Error ? err.message : t('common.error.body')
}
