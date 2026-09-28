import { useFormatters, useT } from '@hikrad/shared'

/**
 * The staleness marker every FR-140 tab shows instead of an error page when
 * the router is unreachable or the cache is simply older than the worker's
 * refresh cadence (gate item 2: cached state renders, never a blank tab).
 */
export function StaleBadge({ stale, readAt }: { stale: boolean; readAt: string | null }) {
  const t = useT()
  const { formatDate } = useFormatters()
  if (!readAt) {
    return <span className="text-xs text-ink-muted">{t('nas.device.neverSampled')}</span>
  }
  if (!stale) {
    return (
      <span className="text-xs text-ink-muted">
        {t('nas.device.readAt', { when: formatDate(readAt) })}
      </span>
    )
  }
  return (
    <span className="inline-flex items-center gap-1 rounded-full bg-warn/15 px-2 py-0.5 text-xs font-medium text-warn">
      {t('nas.device.stale', { when: formatDate(readAt) })}
    </span>
  )
}
