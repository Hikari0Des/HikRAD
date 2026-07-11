import { ErrorState, LoadingState, useFormatters, useT } from '@hikrad/shared'

import { getHealth, type Health } from '../../api/monitoring'
import { PageHeader } from '../../components/PageHeader'
import { useAsync } from '../../hooks/useAsync'

/** Admin health page (FR-35): services, queue, counter invariant, disk. */
export function HealthPage() {
  const t = useT()
  const q = useAsync<Health>(getHealth, [])

  if (q.error) return <ErrorState onRetry={q.reload} />
  if (q.loading || !q.data) return <LoadingState />
  const h = q.data

  return (
    <section className="space-y-4">
      <PageHeader title={t('health.title')} />

      <div className="grid grid-cols-2 gap-2 sm:grid-cols-4">
        <ServiceTile label={t('health.api')} up={h.api.up} />
        <ServiceTile label={t('health.db')} up={h.db.up} />
        <ServiceTile label={t('health.redis')} up={h.redis.up} />
        <ServiceTile label={t('health.freeradius')} up={h.freeradius.up} />
      </div>

      <div className="rounded-md border border-surface-sunken bg-surface-raised p-4">
        <h2 className="mb-2 text-sm font-semibold">{t('health.pipelineTitle')}</h2>
        {/* The lossless-accounting counter invariant is the core product claim. */}
        <div
          className={`mb-3 inline-block rounded px-2 py-1 text-sm font-medium ${
            h.queue.invariant_ok ? 'bg-ok/10 text-ok' : 'bg-danger/10 text-danger'
          }`}
        >
          {h.queue.invariant_ok ? t('health.invariantOk') : t('health.invariantBroken')}
        </div>
        <dl className="grid grid-cols-2 gap-3 text-sm sm:grid-cols-3">
          <Metric label={t('health.queueDepth')} value={String(h.queue.depth)} />
          <Metric label={t('health.drainRate')} value={String(h.queue.drain_rate)} />
          <Metric
            label={t('health.enforceFailures')}
            value={String(h.queue.enforcement_failures)}
          />
        </dl>
      </div>

      <div className="rounded-md border border-surface-sunken bg-surface-raised p-4">
        <h2 className="mb-2 text-sm font-semibold">{t('health.diskTitle')}</h2>
        <ul className="space-y-2">
          {h.disk.map((d) => (
            <DiskRow
              key={d.path}
              path={d.path}
              usedPercent={d.used_percent}
              freeBytes={d.free_bytes}
            />
          ))}
        </ul>
      </div>
    </section>
  )
}

function ServiceTile({ label, up }: { label: string; up: boolean }) {
  const t = useT()
  return (
    <div className="rounded-md border border-surface-sunken bg-surface-raised p-3">
      <p className="text-xs text-ink-muted">{label}</p>
      <p
        className={`mt-1 flex items-center gap-1.5 text-sm font-medium ${up ? 'text-ok' : 'text-danger'}`}
      >
        <span
          className={`inline-block h-2 w-2 rounded-full ${up ? 'bg-ok' : 'bg-danger'}`}
          aria-hidden="true"
        />
        {up ? t('health.up') : t('health.down')}
      </p>
    </div>
  )
}

function Metric({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <dt className="text-xs text-ink-muted">{label}</dt>
      <dd className="mt-0.5 font-medium">{value}</dd>
    </div>
  )
}

function DiskRow({
  path,
  usedPercent,
  freeBytes,
}: {
  path: string
  usedPercent: number
  freeBytes: number
}) {
  const t = useT()
  const { formatNumber } = useFormatters()
  const critical = usedPercent >= 90
  return (
    <li>
      <div className="mb-1 flex items-center justify-between text-sm">
        <code>{path}</code>
        <span className={critical ? 'text-danger' : 'text-ink-muted'}>
          {t('health.usedPercent', { n: formatNumber(usedPercent) })} ·{' '}
          {t('health.freeGb', { n: formatNumber(Math.round(freeBytes / 1e9)) })}
        </span>
      </div>
      <div className="h-2 w-full overflow-hidden rounded-full bg-surface-sunken">
        <div
          className={`h-full ${critical ? 'bg-danger' : 'bg-brand'}`}
          style={{ inlineSize: `${Math.min(usedPercent, 100)}%` }}
        />
      </div>
    </li>
  )
}
