import { useState } from 'react'

import { useFormatters, useT } from '@hikrad/shared'

import { requestLicenseBlob, uploadLicense } from '../../api/setup'
import { ApiError } from '../../api/client'
import { useAuth } from '../../auth/AuthContext'
import { PERM_LICENSE_MANAGE } from '../../auth/permissions'
import { Button } from '../../components/Button'
import { CopyButton } from '../../components/CopyButton'
import { Field, TextInput, Textarea } from '../../components/form'
import { PageHeader } from '../../components/PageHeader'
import { useToast } from '../../components/Toast'
import { useLicense } from '../../license/LicenseContext'

/** License page (FR-50): state, fingerprint, upload a new key, request a re-issue blob. */
export function LicensePage() {
  const t = useT()
  const { formatDate } = useFormatters()
  const { toast } = useToast()
  const { can } = useAuth()
  const { license, reload, isReadOnly } = useLicense()
  const canManage = can(PERM_LICENSE_MANAGE)

  const [payload, setPayload] = useState('')
  const [signature, setSignature] = useState('')
  const [busy, setBusy] = useState(false)

  async function submitUpload() {
    setBusy(true)
    try {
      const parsed = JSON.parse(payload) as unknown
      await uploadLicense(parsed, signature.trim())
      toast(t('license.uploaded'), 'ok')
      setPayload('')
      setSignature('')
      reload()
    } catch (err) {
      toast(err instanceof ApiError ? err.message : t('license.invalidJson'), 'danger')
    } finally {
      setBusy(false)
    }
  }

  async function onFile(e: React.ChangeEvent<HTMLInputElement>) {
    const file = e.target.files?.[0]
    if (!file) return
    try {
      const parsed = JSON.parse(await file.text()) as { payload?: unknown; signature?: string }
      if (parsed.payload && parsed.signature) {
        setPayload(JSON.stringify(parsed.payload))
        setSignature(parsed.signature)
      }
    } catch {
      toast(t('license.invalidJson'), 'danger')
    }
  }

  async function downloadRequestBlob() {
    try {
      const res = await requestLicenseBlob()
      const blob = new Blob([JSON.stringify(res, null, 2)], { type: 'application/json' })
      const url = URL.createObjectURL(blob)
      const a = document.createElement('a')
      a.href = url
      a.download = `hikrad-license-request-${res.fingerprint}.json`
      document.body.appendChild(a)
      a.click()
      a.remove()
      setTimeout(() => URL.revokeObjectURL(url), 1000)
    } catch (err) {
      toast(err instanceof Error ? err.message : t('common.error.body'), 'danger')
    }
  }

  return (
    <section className="max-w-lg">
      <PageHeader title={t('license.title')} />

      {isReadOnly ? (
        <div className="mb-4 rounded-md bg-danger/10 p-3 text-sm text-danger">
          {t('license.readOnlyNotice')}
        </div>
      ) : null}

      <div className="mb-6 space-y-2 rounded-md border border-surface-sunken bg-surface-raised p-4">
        <Row
          label={t('license.state')}
          value={license ? t(`license.state.${license.state}`) : '…'}
        />
        {license?.licensee ? <Row label={t('license.licensee')} value={license.licensee} /> : null}
        {license?.tier ? <Row label={t('license.tier')} value={license.tier} /> : null}
        {license?.max_subscribers ? (
          <Row label={t('license.maxSubscribers')} value={String(license.max_subscribers)} />
        ) : null}
        {license?.grace_expires_at ? (
          <Row label={t('license.graceExpires')} value={formatDate(license.grace_expires_at)} />
        ) : null}
        <div>
          <span className="text-sm text-ink-muted">{t('license.fingerprint')}</span>
          <div className="mt-1 flex items-center gap-2">
            <code dir="ltr" className="flex-1 rounded-md bg-surface-sunken px-3 py-2 text-xs">
              {license?.fingerprint ?? '…'}
            </code>
            <CopyButton text={license?.fingerprint ?? ''} />
          </div>
        </div>
      </div>

      {canManage ? (
        <>
          <div className="mb-6 space-y-3 rounded-md border border-surface-sunken p-4">
            <h2 className="text-sm font-semibold">{t('license.upload.title')}</h2>
            <input type="file" accept=".json" onChange={(e) => void onFile(e)} />
            <Field label={t('license.upload.payload')}>
              <Textarea
                rows={4}
                dir="ltr"
                value={payload}
                onChange={(e) => setPayload(e.target.value)}
              />
            </Field>
            <Field label={t('license.upload.signature')}>
              <TextInput
                dir="ltr"
                value={signature}
                onChange={(e) => setSignature(e.target.value)}
              />
            </Field>
            <Button disabled={busy || !payload || !signature} onClick={() => void submitUpload()}>
              {busy ? t('ui.working') : t('license.upload.submit')}
            </Button>
          </div>

          <div className="rounded-md border border-surface-sunken p-4">
            <h2 className="mb-2 text-sm font-semibold">{t('license.requestBlob.title')}</h2>
            <p className="mb-3 text-sm text-ink-muted">{t('license.requestBlob.body')}</p>
            <Button variant="secondary" onClick={() => void downloadRequestBlob()}>
              {t('license.requestBlob.download')}
            </Button>
          </div>
        </>
      ) : null}
    </section>
  )
}

function Row({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex items-center justify-between gap-2 text-sm">
      <span className="text-ink-muted">{label}</span>
      <span className="font-medium">{value}</span>
    </div>
  )
}
