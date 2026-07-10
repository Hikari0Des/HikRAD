import { Ltr, ErrorState, LoadingState, useFormatters, useT } from '@hikrad/shared'

import { listAudit } from '../../api/audit'
import type { AuditEntry } from '../../api/types'
import { useAuth } from '../../auth/AuthContext'
import { Button } from '../../components/Button'
import { usePaginated } from '../../hooks/usePaginated'

/**
 * Change trail for a subscriber (A's audit-log endpoint). Gated by audit.view —
 * agents don't hold it, so the panel hides the tab for them. Actions are shown
 * with their raw key wrapped LTR (audit keys are stable identifiers, not copy).
 */
export function AuditTrail({ subscriberId }: { subscriberId: string }) {
  const t = useT()
  const { formatDate } = useFormatters()
  const { can } = useAuth()
  const page = usePaginated<AuditEntry>(
    (cursor) =>
      listAudit({ entity_type: 'subscriber', entity_id: subscriberId, cursor, limit: 20 }),
    `audit:${subscriberId}`,
  )

  if (!can('audit.view')) {
    return <p className="py-6 text-center text-sm text-ink-muted">{t('audit.noPermission')}</p>
  }
  if (page.error) return <ErrorState onRetry={page.reset} />
  if (page.loading && page.items.length === 0) return <LoadingState />
  if (page.items.length === 0)
    return <p className="py-6 text-center text-sm text-ink-muted">{t('audit.empty')}</p>

  return (
    <div>
      <ul className="space-y-2">
        {page.items.map((e) => (
          <li
            key={e.id}
            className="flex flex-wrap items-center justify-between gap-2 rounded-md border border-surface-sunken p-3 text-sm"
          >
            <span className="font-medium">
              <Ltr>{e.action}</Ltr>
            </span>
            <span className="text-xs text-ink-muted">
              {e.ip ? <Ltr>{e.ip}</Ltr> : null} · {formatDate(e.at)}
            </span>
          </li>
        ))}
      </ul>
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
