import { Ltr, ErrorState, LoadingState, useFormatters, useT } from '@hikrad/shared'

import { listSessionHistory } from '../../api/live'
import type { SessionHistory as SessionRow } from '../../api/types'
import { Button } from '../../components/Button'
import { usePaginated } from '../../hooks/usePaginated'
import { formatBytes } from '../../lib/units'

/** Session-history table (FR-31) with stale/reaped badges and a service tag. */
export function SessionHistory({ subscriberId }: { subscriberId: string }) {
  const t = useT()
  const { formatNumber, formatDate } = useFormatters()
  const page = usePaginated<SessionRow>(
    (cursor) => listSessionHistory({ subscriber_id: subscriberId, cursor, limit: 25 }),
    `history:${subscriberId}`,
  )

  if (page.error) return <ErrorState onRetry={page.reset} />
  if (page.loading && page.items.length === 0) return <LoadingState />
  if (page.items.length === 0)
    return <p className="py-6 text-center text-sm text-ink-muted">{t('history.empty')}</p>

  return (
    <div>
      <div className="overflow-x-auto rounded-md border border-surface-sunken">
        <table className="w-full min-w-[640px] text-sm">
          <thead className="bg-surface-raised text-xs uppercase tracking-wide text-ink-muted">
            <tr>
              <th className="px-3 py-2 text-start font-medium">{t('history.started')}</th>
              <th className="px-3 py-2 text-start font-medium">{t('history.stopped')}</th>
              <th className="px-3 py-2 text-start font-medium">{t('history.ip')}</th>
              <th className="px-3 py-2 text-start font-medium">{t('history.usage')}</th>
              <th className="px-3 py-2 text-start font-medium">{t('history.cause')}</th>
              <th className="px-3 py-2 text-start font-medium">{t('history.service')}</th>
            </tr>
          </thead>
          <tbody>
            {page.items.map((s) => (
              <tr key={s.id} className="border-t border-surface-sunken">
                <td className="whitespace-nowrap px-3 py-2">
                  {s.started_at ? formatDate(s.started_at) : '—'}
                </td>
                <td className="whitespace-nowrap px-3 py-2">
                  {s.stopped_at ? formatDate(s.stopped_at) : t('history.ongoing')}
                </td>
                <td className="whitespace-nowrap px-3 py-2">
                  <Ltr>{s.ip || '—'}</Ltr>
                </td>
                <td className="whitespace-nowrap px-3 py-2">
                  ↓ {formatBytes(s.bytes_in, formatNumber)} · ↑{' '}
                  {formatBytes(s.bytes_out, formatNumber)}
                </td>
                <td className="whitespace-nowrap px-3 py-2">
                  <span className="flex items-center gap-1.5">
                    {s.terminate_cause ? <Ltr>{s.terminate_cause}</Ltr> : '—'}
                    {s.reaped && (
                      <span
                        className="rounded bg-danger/10 px-1.5 py-0.5 text-xs text-danger"
                        title={t('history.reapedHint')}
                      >
                        {t('history.reaped')}
                      </span>
                    )}
                    {s.stale && !s.reaped && (
                      <span className="rounded bg-surface-sunken px-1.5 py-0.5 text-xs text-ink-muted">
                        {t('history.stale')}
                      </span>
                    )}
                  </span>
                </td>
                <td className="whitespace-nowrap px-3 py-2">
                  <span className="rounded bg-surface-sunken px-1.5 py-0.5 text-xs">
                    {t(`live.service.${s.service}`)}
                  </span>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      {page.hasMore && (
        <div className="mt-3 text-center">
          <Button size="sm" variant="secondary" disabled={page.loading} onClick={page.loadMore}>
            {page.loading ? t('common.loading') : t('ui.loadMore')}
          </Button>
        </div>
      )}
    </div>
  )
}
