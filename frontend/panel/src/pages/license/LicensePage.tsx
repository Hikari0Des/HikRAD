import { useState } from 'react'

import { useFormatters, useT } from '@hikrad/shared'

import { requestLicenseBlob, uploadLicense, type Entitlement } from '../../api/setup'
import { ApiError } from '../../api/client'
import { useAuth } from '../../auth/AuthContext'
import { PERM_LICENSE_MANAGE } from '../../auth/permissions'
import { Button } from '../../components/Button'
import { CopyButton } from '../../components/CopyButton'
import { TextInput, Textarea } from '../../components/form'
import { FormField } from '../../components/patterns'
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

  // FR-104: name which fingerprint component drifted, so the operator knows
  // whether a re-issue is needed (both changed) or the install is still within
  // tolerance (one changed). Null when nothing has drifted.
  const fm = license?.fingerprint_match
  const driftKey =
    fm && !(fm.machine_id_match && fm.mac_match)
      ? !fm.machine_id_match && !fm.mac_match
        ? 'both'
        : !fm.machine_id_match
          ? 'machineId'
          : 'mac'
      : null

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
      a.download = `hikrad-activation-request-${res.fingerprint}.json`
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

      {license?.over_cap ? (
        <div className="mb-4 rounded-md bg-warn/10 p-3 text-sm text-warn">
          {t('license.overCap', {
            count: String(license.subscriber_count ?? ''),
            cap: String(license.max_subscribers ?? ''),
          })}
        </div>
      ) : null}

      <div className="mb-6 space-y-2 rounded-md border border-border bg-surface-raised p-4">
        {/* Status banner: green when valid, amber with a countdown while in
            grace, red once grace has expired — so the state and (for grace)
            how long is left read at a glance, not just the word "valid". */}
        <div
          className={`mb-1 rounded-md px-3 py-2 text-sm font-semibold ${
            !license
              ? 'bg-surface-sunken text-ink-muted'
              : license.state === 'valid'
                ? 'bg-ok/10 text-ok'
                : license.state === 'grace'
                  ? 'bg-warn/10 text-warn'
                  : 'bg-danger/10 text-danger'
          }`}
        >
          {license ? t(`license.state.${license.state}`) : '…'}
          {license?.state === 'grace' && graceDaysLeft(license.grace_expires_at) != null ? (
            <span className="font-normal">
              {' — '}
              {t('license.graceRemaining', { n: String(graceDaysLeft(license.grace_expires_at)) })}
            </span>
          ) : null}
        </div>
        {license?.licensee ? <Row label={t('license.licensee')} value={license.licensee} /> : null}
        {license?.tier ? <Row label={t('license.tier')} value={license.tier} /> : null}
        {license?.max_subscribers ? (
          <Row
            label={t('license.maxSubscribers')}
            value={
              license.subscriber_count != null
                ? `${license.subscriber_count} / ${license.max_subscribers}`
                : String(license.max_subscribers)
            }
          />
        ) : null}
        {license?.entitled_version ? (
          <Row label={t('license.entitledVersion')} value={license.entitled_version} />
        ) : null}
        {license?.key_id ? <Row label={t('license.keyId')} value={license.key_id} /> : null}
        {license?.serial ? <Row label={t('license.serial')} value={license.serial} /> : null}
        {license?.grace_expires_at ? (
          <Row label={t('license.graceExpires')} value={formatDate(license.grace_expires_at)} />
        ) : null}
        {license?.expires_at ? (
          <Row label={t('license.expiresAt')} value={formatDate(license.expires_at)} />
        ) : null}
        {license?.support_until ? (
          <Row label={t('license.supportUntil')} value={formatDate(license.support_until)} />
        ) : null}
        {license?.entitlements?.length ? (
          <div>
            <span className="text-sm text-ink-muted">{t('license.entitlements')}</span>
            <div className="mt-1 flex flex-wrap gap-1.5">
              {license.entitlements.map((e: Entitlement) => (
                <span
                  key={e}
                  className="rounded-full bg-surface-sunken px-2.5 py-0.5 text-xs font-medium text-ink"
                >
                  {t(`license.entitlement.${e}`)}
                </span>
              ))}
            </div>
          </div>
        ) : null}
        <div>
          <span className="text-sm text-ink-muted">{t('license.fingerprint')}</span>
          <div className="mt-1 flex items-center gap-2">
            <code dir="ltr" className="flex-1 rounded-md bg-surface-sunken px-3 py-2 text-xs">
              {license?.fingerprint ?? '…'}
            </code>
            <CopyButton text={license?.fingerprint ?? ''} />
          </div>
          {driftKey ? (
            <p className="mt-2 text-xs text-warn">{t(`license.fingerprintDrift.${driftKey}`)}</p>
          ) : null}
        </div>
      </div>

      {canManage ? (
        <>
          {/* FR-123/FR-125: export-request and import-license are the two
              first-class actions of the file exchange — buttons, not
              documentation. Manual payload/signature entry (the CI/fallback
              path) is a disclosure below, not the primary form. */}
          <div className="mb-6 rounded-md border border-border p-4">
            <h2 className="mb-2 text-sm font-semibold">{t('license.requestBlob.title')}</h2>
            <p className="mb-3 text-sm text-ink-muted">{t('license.requestBlob.body')}</p>
            <Button variant="secondary" onClick={() => void downloadRequestBlob()}>
              {t('license.requestBlob.download')}
            </Button>
          </div>

          <div className="mb-6 space-y-3 rounded-md border border-border p-4">
            <h2 className="text-sm font-semibold">{t('license.upload.title')}</h2>
            <p className="text-sm text-ink-muted">{t('license.upload.body')}</p>
            <input type="file" accept=".json" onChange={(e) => void onFile(e)} />
            <details className="text-sm">
              <summary className="cursor-pointer text-ink-muted">
                {t('license.upload.manualEntry')}
              </summary>
              <div className="mt-3 space-y-3">
                <FormField label={t('license.upload.payload')}>
                  {(id) => (
                    <Textarea
                      id={id}
                      rows={4}
                      dir="ltr"
                      value={payload}
                      onChange={(e) => setPayload(e.target.value)}
                    />
                  )}
                </FormField>
                <FormField label={t('license.upload.signature')}>
                  {(id) => (
                    <TextInput
                      id={id}
                      dir="ltr"
                      value={signature}
                      onChange={(e) => setSignature(e.target.value)}
                    />
                  )}
                </FormField>
              </div>
            </details>
            <Button disabled={busy || !payload || !signature} onClick={() => void submitUpload()}>
              {busy ? t('ui.working') : t('license.upload.submit')}
            </Button>
          </div>
        </>
      ) : null}
    </section>
  )
}

// Whole days from now until the grace window closes (never negative). Null when
// there is no grace expiry — the banner then just shows the state word.
function graceDaysLeft(iso?: string): number | null {
  if (!iso) return null
  const ms = new Date(iso).getTime() - Date.now()
  return Math.max(0, Math.ceil(ms / 86_400_000))
}

function Row({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex items-center justify-between gap-2 text-sm">
      <span className="text-ink-muted">{label}</span>
      <span className="font-medium">{value}</span>
    </div>
  )
}
