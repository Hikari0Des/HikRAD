import { useState } from 'react'

import { ErrorState, LoadingState, useFormatters, useT } from '@hikrad/shared'

import { listPanelSessions, revokePanelSession, type PanelSession } from '../../api/security'
import { Button } from '../../components/Button'
import { PageHeader } from '../../components/PageHeader'
import { useToast } from '../../components/Toast'
import { useAsync } from '../../hooks/useAsync'
import { TotpEnroll } from './TotpEnroll'

/** Self-service security (FR-28/FR-29): TOTP enrolment + active panel sessions. */
export function AccountSecurityPage() {
  const t = useT()
  return (
    <section className="space-y-6">
      <PageHeader title={t('account.title')} />

      <div className="rounded-md border border-surface-sunken bg-surface-raised p-4">
        <h2 className="mb-1 text-sm font-semibold">{t('account.twofaTitle')}</h2>
        <p className="mb-3 text-xs text-ink-muted">{t('account.twofaHint')}</p>
        <EnrolSection />
      </div>

      <div className="rounded-md border border-surface-sunken bg-surface-raised p-4">
        <h2 className="mb-3 text-sm font-semibold">{t('account.sessionsTitle')}</h2>
        <SessionsList />
      </div>
    </section>
  )
}

function EnrolSection() {
  const t = useT()
  const [done, setDone] = useState(false)
  const [started, setStarted] = useState(false)

  if (done) return <p className="text-sm text-ok">{t('totp.enrolled')}</p>
  if (!started) {
    return (
      <Button size="sm" onClick={() => setStarted(true)}>
        {t('account.enableTwofa')}
      </Button>
    )
  }
  return <TotpEnroll onEnrolled={() => setDone(true)} />
}

function SessionsList() {
  const t = useT()
  const { formatDate } = useFormatters()
  const { toast } = useToast()
  const q = useAsync<{ items: PanelSession[] }>(() => listPanelSessions(), [])

  if (q.error) return <ErrorState onRetry={q.reload} />
  if (q.loading || !q.data) return <LoadingState />

  async function revoke(id: string) {
    try {
      await revokePanelSession(id)
      toast(t('account.sessionRevoked'), 'ok')
      q.reload()
    } catch (err) {
      toast(err instanceof Error ? err.message : t('common.error.body'), 'danger')
    }
  }

  return (
    <ul className="space-y-2">
      {q.data.items.map((s) => (
        <li
          key={s.id}
          className="flex items-center justify-between gap-3 border-b border-surface-sunken/60 py-2 text-sm"
        >
          <div className="min-w-0">
            <p className="truncate">
              <bdi dir="ltr">{s.ua || t('account.unknownDevice')}</bdi>
              {s.current ? (
                <span className="ms-2 rounded bg-brand-soft px-1.5 py-0.5 text-xs text-brand-strong">
                  {t('account.thisDevice')}
                </span>
              ) : null}
            </p>
            <p className="text-xs text-ink-muted">
              <bdi dir="ltr">{s.ip}</bdi> · {formatDate(s.created_at)}
            </p>
          </div>
          {!s.current ? (
            <Button size="sm" variant="ghost" onClick={() => void revoke(s.id)}>
              {t('account.revoke')}
            </Button>
          ) : null}
        </li>
      ))}
    </ul>
  )
}
