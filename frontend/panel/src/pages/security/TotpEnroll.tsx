import { useState } from 'react'

import { useT } from '@hikrad/shared'

import { triggerDownload } from '../../api/billing'
import { enrollTotp, verifyTotp, type EnrollResponse } from '../../api/security'
import { Button } from '../../components/Button'
import { Field, TextInput } from '../../components/form'
import { CopyButton } from '../../components/CopyButton'
import { useAsync } from '../../hooks/useAsync'
import { ErrorState, LoadingState } from '@hikrad/shared'

/**
 * TOTP enrolment panel (FR-28.1), reused by self-service (Security page) and the
 * forced-enrolment login step. Shows the authenticator setup key for manual
 * entry (the otpauth URI is provided for apps that scan; the secret is always
 * shown per the edge case), verifies the first code, then reveals one-time
 * backup codes with a download. `enrollmentToken` is passed only on the
 * login-forced path (self-service uses the access token).
 */
export function TotpEnroll({
  enrollmentToken,
  onEnrolled,
}: {
  enrollmentToken?: string
  onEnrolled: () => void
}) {
  const t = useT()
  const enroll = useAsync<EnrollResponse>(() => enrollTotp(enrollmentToken), [])
  const [code, setCode] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [backupCodes, setBackupCodes] = useState<string[] | null>(null)

  if (enroll.error) return <ErrorState onRetry={enroll.reload} />
  if (enroll.loading || !enroll.data) return <LoadingState />

  async function verify() {
    setBusy(true)
    setError(null)
    try {
      const res = await verifyTotp(code.trim(), enrollmentToken)
      setBackupCodes(res.backup_codes)
    } catch {
      setError(t('totp.error.invalidCode'))
    } finally {
      setBusy(false)
    }
  }

  if (backupCodes) {
    return (
      <div className="space-y-4">
        <div className="rounded-md bg-ok/10 p-3 text-sm text-ok">{t('totp.enrolled')}</div>
        <div>
          <p className="text-sm font-medium">{t('totp.backupTitle')}</p>
          <p className="mt-0.5 text-xs text-ink-muted">{t('totp.backupHint')}</p>
          <ul className="mt-2 grid grid-cols-2 gap-1 rounded-md border border-surface-sunken bg-surface p-3">
            {backupCodes.map((c) => (
              <li key={c}>
                <code className="text-sm">{c}</code>
              </li>
            ))}
          </ul>
          <div className="mt-2 flex gap-2">
            <Button
              size="sm"
              variant="secondary"
              onClick={() =>
                triggerDownload(
                  backupCodes.join('\n') + '\n',
                  'hikrad-backup-codes.txt',
                  'text/plain',
                )
              }
            >
              {t('totp.downloadBackup')}
            </Button>
            <Button size="sm" onClick={onEnrolled}>
              {t('ui.done')}
            </Button>
          </div>
        </div>
      </div>
    )
  }

  return (
    <div className="space-y-4">
      <div>
        <p className="text-sm font-medium">{t('totp.manualKey')}</p>
        <p className="mt-0.5 text-xs text-ink-muted">{t('totp.manualKeyHint')}</p>
        <div className="mt-2 flex items-center gap-2">
          <code className="grow break-all rounded-md border border-surface-sunken bg-surface px-3 py-2 text-sm">
            {enroll.data.secret}
          </code>
          <CopyButton text={enroll.data.secret} />
        </div>
        <details className="mt-2 text-xs text-ink-muted">
          <summary className="cursor-pointer">{t('totp.showUri')}</summary>
          <code className="mt-1 block break-all">{enroll.data.otpauth_uri}</code>
        </details>
      </div>

      <form
        onSubmit={(e) => {
          e.preventDefault()
          void verify()
        }}
        className="space-y-3"
      >
        <Field label={t('totp.enterCode')} error={error ?? undefined} htmlFor="totp-code">
          <TextInput
            id="totp-code"
            inputMode="numeric"
            autoComplete="one-time-code"
            dir="ltr"
            value={code}
            onChange={(e) => setCode(e.target.value)}
          />
        </Field>
        <Button type="submit" disabled={busy || code.trim().length < 6}>
          {busy ? t('ui.working') : t('totp.verify')}
        </Button>
      </form>
    </div>
  )
}
