import { useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'

import { EmptyState, ErrorState, LoadingState, useFormatters, useT } from '@hikrad/shared'

import {
  getTrafficByEntity,
  type TrafficEntityRow,
  type TrafficEntityView,
} from '../../api/reports'
import { Button } from '../../components/Button'
import { TextInput } from '../../components/form'
import { PageHeader } from '../../components/PageHeader'
import { useAsync } from '../../hooks/useAsync'
import { useDebouncedValue } from '../../hooks/useDebouncedValue'
import { formatBytes } from '../../lib/units'
import { PrintHeader } from './PrintHeader'

const VIEWS: TrafficEntityView[] = ['users', 'cards']

/** FR-173: traffic by entity, separate Users/Cards tabs — never one query
 * shared between them (gate item 2: a card's traffic must never appear in
 * the Users tab and vice versa; see backend/internal/reports/traffic_entity.go). */
export function TrafficByEntityPage() {
  const t = useT()
  const { formatNumber } = useFormatters()
  const [params, setParams] = useSearchParams()
  const view = (params.get('view') as TrafficEntityView) || 'users'
  const [q, setQ] = useState('')
  const debouncedQ = useDebouncedValue(q, 300)
  const [cursorStack, setCursorStack] = useState<(string | null)[]>([null])
  const cursor = cursorStack[cursorStack.length - 1]

  function setView(v: TrafficEntityView) {
    const next = new URLSearchParams(params)
    next.set('view', v)
    setParams(next, { replace: true })
    setCursorStack([null])
  }

  const { data, error, loading, reload } = useAsync(
    () => getTrafficByEntity(view, debouncedQ, cursor),
    [view, debouncedQ, cursor],
  )
  const items: TrafficEntityRow[] = data?.items ?? []

  function nextPage() {
    if (data?.next_cursor) setCursorStack((s) => [...s, data.next_cursor!])
  }
  function prevPage() {
    setCursorStack((s) => (s.length > 1 ? s.slice(0, -1) : s))
  }

  return (
    <section className="print-report">
      <PrintHeader reportTitle={t('reports.trafficByEntity.title')} />
      <PageHeader
        title={t('reports.trafficByEntity.title')}
        subtitle={t('reports.trafficByEntity.subtitle')}
      />
      <div className="mb-4 flex flex-wrap items-center justify-between gap-2 print:hidden">
        <div className="flex overflow-hidden rounded-md border border-border">
          {VIEWS.map((v) => (
            <button
              key={v}
              type="button"
              onClick={() => setView(v)}
              className={`px-3 py-1.5 text-xs font-medium ${
                view === v
                  ? 'bg-brand text-ink-inverse'
                  : 'bg-surface text-ink hover:bg-surface-sunken'
              }`}
            >
              {t(`reports.trafficByEntity.view.${v}`)}
            </button>
          ))}
        </div>
        <TextInput
          value={q}
          onChange={(e) => {
            setQ(e.target.value)
            setCursorStack([null])
          }}
          placeholder={t('reports.trafficByEntity.search')}
          className="w-auto"
          aria-label={t('reports.trafficByEntity.search')}
        />
      </div>

      {error ? (
        <ErrorState onRetry={reload} />
      ) : loading ? (
        <LoadingState />
      ) : items.length === 0 ? (
        <EmptyState title={t('reports.empty.title')} body={t('reports.empty.body')} />
      ) : (
        <>
          <div className="overflow-x-auto rounded-md border border-border">
            <table className="w-full text-sm">
              <thead className="bg-surface-sunken/40 text-start text-xs text-ink-muted">
                <tr>
                  <th className="px-3 py-2 text-start font-medium">
                    {view === 'users'
                      ? t('reports.col.username')
                      : t('reports.trafficByEntity.col.group')}
                  </th>
                  <th className="px-3 py-2 text-end font-medium">
                    {t('reports.trafficByEntity.col.today')}
                  </th>
                  <th className="px-3 py-2 text-end font-medium">
                    {t('reports.trafficByEntity.col.month')}
                  </th>
                </tr>
              </thead>
              <tbody>
                {items.map((r) => (
                  <tr key={r.id} className="border-t border-border/60">
                    <td className="px-3 py-2">
                      <Link
                        to={view === 'users' ? `/subscribers/${r.id}` : '/cards'}
                        className="text-brand hover:underline print:no-underline print:text-ink"
                      >
                        {r.name}
                      </Link>
                    </td>
                    <td className="px-3 py-2 text-end">
                      {formatBytes(r.today_down_bytes + r.today_up_bytes, formatNumber)}
                    </td>
                    <td className="px-3 py-2 text-end">
                      {formatBytes(r.month_down_bytes + r.month_up_bytes, formatNumber)}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          <div className="mt-3 flex justify-end gap-2 print:hidden">
            <Button
              size="sm"
              variant="secondary"
              disabled={cursorStack.length <= 1}
              onClick={prevPage}
            >
              {t('reports.trafficByEntity.prev')}
            </Button>
            <Button size="sm" variant="secondary" disabled={!data?.next_cursor} onClick={nextPage}>
              {t('reports.trafficByEntity.next')}
            </Button>
          </div>
        </>
      )}
    </section>
  )
}
