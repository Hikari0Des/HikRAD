import { useState } from 'react'

import { useT } from '@hikrad/shared'

import {
  dryRunBatch,
  getBatchJob,
  startBatch,
  type BatchDryRunResult,
  type BatchInput,
  type BatchJob,
} from '../../api/subscribers'
import { ApiError } from '../../api/client'
import type { Profile } from '../../api/types'
import { Button } from '../../components/Button'
import { Checkbox, Field, Select, TextInput } from '../../components/form'
import { Modal } from '../../components/Modal'
import { useToast } from '../../components/Toast'

type Step = 'form' | 'dryrun' | 'running' | 'summary'

/**
 * FR-133 batch/numbered creation: prefix + numeric range → dry-run (exact
 * usernames + collisions) → create (per-row independent, licence-cap
 * pre-checked) → summary, mirroring the bulk-action job/poll shape.
 */
export function BatchCreateModal({
  open,
  onOpenChange,
  profiles,
  onDone,
}: {
  open: boolean
  onOpenChange: (o: boolean) => void
  profiles: Profile[]
  onDone: () => void
}) {
  const t = useT()
  const { toast } = useToast()
  const [step, setStep] = useState<Step>('form')
  const [prefix, setPrefix] = useState('')
  const [startIndex, setStartIndex] = useState(1)
  const [count, setCount] = useState(50)
  const [digitPadding, setDigitPadding] = useState(0)
  const [passwordMode, setPasswordMode] = useState<'shared' | 'generated'>('generated')
  const [sharedPassword, setSharedPassword] = useState('')
  const [profileId, setProfileId] = useState('')
  const [serviceType, setServiceType] = useState<'pppoe' | 'hotspot' | 'dual'>('pppoe')
  const [invoicing, setInvoicing] = useState<'each' | 'batch' | 'none'>('none')
  const [autoRenew, setAutoRenew] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [dryRun, setDryRun] = useState<BatchDryRunResult | null>(null)
  const [job, setJob] = useState<BatchJob | null>(null)

  function buildInput(): BatchInput {
    return {
      prefix,
      start_index: startIndex,
      count,
      digit_padding: digitPadding || undefined,
      password_mode: passwordMode,
      shared_password: passwordMode === 'shared' ? sharedPassword : undefined,
      profile_id: profileId,
      service_type: serviceType,
      invoicing,
      auto_renew: autoRenew,
    }
  }

  async function runDryRun() {
    setBusy(true)
    setError(null)
    try {
      const res = await dryRunBatch(buildInput())
      setDryRun(res)
      setStep('dryrun')
    } catch (err) {
      setError(err instanceof ApiError ? err.message : t('common.error.body'))
    } finally {
      setBusy(false)
    }
  }

  async function confirmCreate() {
    setBusy(true)
    setStep('running')
    try {
      const started = await startBatch(buildInput())
      await poll(started.id)
    } catch (err) {
      toast(err instanceof ApiError ? err.message : t('common.error.body'), 'danger')
      setStep('dryrun')
    } finally {
      setBusy(false)
    }
  }

  async function poll(id: string) {
    for (;;) {
      const j = await getBatchJob(id)
      setJob(j)
      if (j.status === 'completed') {
        setStep('summary')
        onDone()
        return
      }
      await new Promise((r) => setTimeout(r, 800))
    }
  }

  function reset() {
    setStep('form')
    setDryRun(null)
    setJob(null)
    setError(null)
    onOpenChange(false)
  }

  return (
    <Modal open={open} onOpenChange={busy ? () => {} : reset} title={t('batch.title')} size="lg">
      {step === 'form' && (
        <form
          onSubmit={(e) => {
            e.preventDefault()
            void runDryRun()
          }}
          className="space-y-4"
        >
          <div className="grid grid-cols-3 gap-3">
            <Field label={t('batch.prefix')} htmlFor="bc-prefix">
              <TextInput
                id="bc-prefix"
                value={prefix}
                onChange={(e) => setPrefix(e.target.value)}
                dir="ltr"
              />
            </Field>
            <Field label={t('batch.startIndex')} htmlFor="bc-start">
              <TextInput
                id="bc-start"
                type="number"
                min={0}
                value={startIndex}
                onChange={(e) => setStartIndex(Number(e.target.value))}
                dir="ltr"
              />
            </Field>
            <Field label={t('batch.count')} htmlFor="bc-count">
              <TextInput
                id="bc-count"
                type="number"
                min={1}
                max={10000}
                value={count}
                onChange={(e) => setCount(Number(e.target.value))}
                dir="ltr"
              />
            </Field>
          </div>
          <Field
            label={t('batch.digitPadding')}
            hint={t('batch.digitPaddingHint')}
            htmlFor="bc-pad"
          >
            <TextInput
              id="bc-pad"
              type="number"
              min={0}
              max={10}
              value={digitPadding}
              onChange={(e) => setDigitPadding(Number(e.target.value))}
              dir="ltr"
            />
          </Field>
          <Field label={t('subscriber.profile')} htmlFor="bc-profile">
            <Select
              id="bc-profile"
              value={profileId}
              onChange={(e) => setProfileId(e.target.value)}
              required
            >
              <option value="" disabled>
                {t('vouchers.pickProfile')}
              </option>
              {profiles.map((p) => (
                <option key={p.id} value={p.id}>
                  {p.name}
                </option>
              ))}
            </Select>
          </Field>
          <div className="grid grid-cols-2 gap-3">
            <Field label={t('batch.passwordMode')} htmlFor="bc-pwmode">
              <Select
                id="bc-pwmode"
                value={passwordMode}
                onChange={(e) => setPasswordMode(e.target.value as 'shared' | 'generated')}
              >
                <option value="generated">{t('batch.passwordGenerated')}</option>
                <option value="shared">{t('batch.passwordShared')}</option>
              </Select>
            </Field>
            {passwordMode === 'shared' && (
              <Field label={t('subscriber.password')} htmlFor="bc-pw">
                <TextInput
                  id="bc-pw"
                  value={sharedPassword}
                  onChange={(e) => setSharedPassword(e.target.value)}
                  dir="ltr"
                  required
                />
              </Field>
            )}
          </div>
          <div className="grid grid-cols-2 gap-3">
            <Field label={t('subscriber.serviceType')} htmlFor="bc-svc">
              <Select
                id="bc-svc"
                value={serviceType}
                onChange={(e) => setServiceType(e.target.value as 'pppoe' | 'hotspot' | 'dual')}
              >
                <option value="pppoe">{t('serviceType.pppoe')}</option>
                <option value="hotspot">{t('serviceType.hotspot')}</option>
                <option value="dual">{t('serviceType.dual')}</option>
              </Select>
            </Field>
            <Field
              label={t('batch.invoicing')}
              hint={t('batch.invoicingHint')}
              htmlFor="bc-invoice"
            >
              <Select
                id="bc-invoice"
                value={invoicing}
                onChange={(e) => setInvoicing(e.target.value as 'each' | 'batch' | 'none')}
              >
                <option value="none">{t('batch.invoiceNone')}</option>
                <option value="each">{t('batch.invoiceEach')}</option>
                <option value="batch">{t('batch.invoiceBatch')}</option>
              </Select>
            </Field>
          </div>
          <Checkbox
            label={t('subscriber.autoRenew')}
            checked={autoRenew}
            onChange={() => setAutoRenew((v) => !v)}
          />
          {error ? <p className="text-sm text-danger">{error}</p> : null}
          <div className="flex justify-end gap-2">
            <Button variant="ghost" disabled={busy} onClick={reset}>
              {t('ui.cancel')}
            </Button>
            <Button type="submit" disabled={busy || !profileId || !prefix}>
              {busy ? t('ui.working') : t('batch.previewDryRun')}
            </Button>
          </div>
        </form>
      )}

      {step === 'dryrun' && dryRun && (
        <div className="space-y-4">
          <p className="text-sm">
            {t('batch.dryRunSummary', {
              total: dryRun.total,
              collisions: dryRun.collisions.length,
            })}
          </p>
          {dryRun.cap_blocked && (
            <p className="rounded-md bg-danger/10 p-3 text-sm text-danger">{dryRun.cap_message}</p>
          )}
          {dryRun.collisions.length > 0 && (
            <div className="max-h-32 overflow-y-auto rounded-md border border-warn/30 bg-warn/5 p-2 text-xs">
              {dryRun.collisions.join(', ')}
            </div>
          )}
          <div className="max-h-48 overflow-y-auto rounded-md border border-border p-2 font-mono text-xs">
            {dryRun.usernames.slice(0, 200).join(', ')}
            {dryRun.usernames.length > 200 ? '…' : ''}
          </div>
          <div className="flex justify-between gap-2">
            <Button variant="ghost" onClick={() => setStep('form')}>
              {t('ui.back')}
            </Button>
            <Button disabled={busy || dryRun.cap_blocked} onClick={() => void confirmCreate()}>
              {t('batch.confirmCreate')}
            </Button>
          </div>
        </div>
      )}

      {step === 'running' && (
        <div className="space-y-3">
          <p className="text-sm text-ink-muted">{t('batch.creating')}</p>
          {job ? (
            <div className="h-2 overflow-hidden rounded-full bg-surface-sunken">
              <div
                className="h-full bg-brand transition-all"
                style={{ width: `${job.total ? Math.round((job.done / job.total) * 100) : 0}%` }}
              />
            </div>
          ) : null}
        </div>
      )}

      {step === 'summary' && job && (
        <div className="space-y-4">
          <p className="text-sm">
            {t('batch.summary', { succeeded: job.succeeded, failed: job.failed })}
          </p>
          {job.passwords && Object.keys(job.passwords).length > 0 && (
            <div className="max-h-56 overflow-y-auto rounded-md border border-border p-2 font-mono text-xs">
              {Object.entries(job.passwords).map(([u, p]) => (
                <div key={u}>
                  {u}: {p}
                </div>
              ))}
              <p className="mt-2 font-sans text-ink-muted">{t('batch.passwordsOnceNote')}</p>
            </div>
          )}
          {job.failures.length > 0 && (
            <div className="max-h-40 overflow-y-auto rounded-md border border-warn/30 bg-warn/5 p-2 text-xs">
              {job.failures.map((f, i) => (
                <div key={i}>
                  {f.username}: {f.error}
                </div>
              ))}
            </div>
          )}
          <div className="flex justify-end">
            <Button onClick={reset}>{t('ui.done')}</Button>
          </div>
        </div>
      )}
    </Modal>
  )
}
