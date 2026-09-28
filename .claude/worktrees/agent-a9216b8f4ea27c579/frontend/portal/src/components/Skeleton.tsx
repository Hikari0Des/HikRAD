import { useT } from '@hikrad/shared'

/** Loading skeleton blocks (never a spinner over a blank page — UX note). */
export function Skeleton({ className }: { className?: string }) {
  return (
    <div
      aria-hidden="true"
      className={`animate-pulse rounded-md bg-surface-sunken${className ? ` ${className}` : ''}`}
    />
  )
}

/** Skeleton shaped like the home answer card, shown while `getMe()` loads. */
export function HomeSkeleton() {
  const t = useT()
  return (
    <div className="flex flex-col gap-4" role="status" aria-label={t('common.loading')}>
      <Skeleton className="h-6 w-32" />
      <div className="flex flex-col gap-3 rounded-xl bg-surface-raised p-4 shadow-sm">
        <Skeleton className="h-5 w-full" />
        <Skeleton className="h-5 w-3/4" />
        <Skeleton className="h-5 w-1/2" />
      </div>
      <div className="flex flex-col gap-2 rounded-xl bg-surface-raised p-4 shadow-sm">
        <Skeleton className="h-4 w-24" />
        <Skeleton className="h-8 w-40" />
      </div>
    </div>
  )
}
