import { type ReactNode } from 'react'

/**
 * v3-1 FR-97.2 detail-page header: title + optional status + meta line + an
 * actions slot. Used by the subscriber / NAS / device detail pages so every
 * detail screen has the same top region. Presentational; RTL-safe via logical
 * utilities.
 */
export function DetailHeader({
  title,
  status,
  meta,
  actions,
}: {
  title: ReactNode
  /** e.g. a <StatusBadge/>. */
  status?: ReactNode
  /** Secondary line: id/owner/created, rendered muted. */
  meta?: ReactNode
  actions?: ReactNode
}) {
  return (
    <div className="mb-4 flex flex-wrap items-start justify-between gap-3">
      <div className="min-w-0">
        <div className="flex flex-wrap items-center gap-2">
          <h1 className="truncate text-xl font-semibold">{title}</h1>
          {status}
        </div>
        {meta ? <div className="mt-1 text-sm text-ink-muted">{meta}</div> : null}
      </div>
      {actions ? <div className="flex flex-wrap items-center gap-2">{actions}</div> : null}
    </div>
  )
}
