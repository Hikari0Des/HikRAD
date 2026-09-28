import { useMemo } from 'react'

import { ChartContainer, useFormatters, useT } from '@hikrad/shared'

import { formatBytes } from '../lib/units'
import type { UsagePoint } from '../api/types'

/**
 * Grouped down/up usage bars per time bucket (FR-33). Charts always render LTR
 * even inside RTL pages (NFR-6.2) — wrapped in <ChartContainer>. Inline SVG, no
 * chart dependency. Axis labels use locale digits via the shared formatters.
 */
export function UsageChart({
  points,
  granularity,
}: {
  points: UsagePoint[]
  granularity: 'daily' | 'monthly'
}) {
  const t = useT()
  const { formatNumber } = useFormatters()

  const max = useMemo(() => Math.max(1, ...points.map((p) => Math.max(p.down, p.up))), [points])

  if (points.length === 0) {
    return <p className="py-8 text-center text-sm text-ink-muted">{t('usage.empty')}</p>
  }

  const width = 640
  const height = 200
  const padBottom = 28
  const padTop = 8
  const chartH = height - padBottom - padTop
  const slot = width / points.length
  const barW = Math.max(2, Math.min(14, slot / 3))

  // Compact axis/tooltip label: month/day for daily, year/month for monthly.
  // Built from parts (locale digits, no time) so daily buckets don't render a
  // meaningless midnight timestamp; stays LTR inside <ChartContainer>.
  const label = (iso: string) => {
    const d = new Date(iso)
    const m = formatNumber(d.getUTCMonth() + 1)
    return granularity === 'monthly'
      ? `${formatNumber(d.getUTCFullYear())}/${m}`
      : `${m}/${formatNumber(d.getUTCDate())}`
  }

  return (
    <div>
      <div className="mb-2 flex items-center gap-4 text-xs text-ink-muted">
        <span className="flex items-center gap-1.5">
          <span aria-hidden="true" className="inline-block h-2.5 w-2.5 rounded-sm bg-brand" />
          {t('usage.download')}
        </span>
        <span className="flex items-center gap-1.5">
          <span
            aria-hidden="true"
            className="inline-block h-2.5 w-2.5 rounded-sm bg-brand-strong"
          />
          {t('usage.upload')}
        </span>
      </div>
      <ChartContainer className="w-full overflow-x-auto">
        <svg
          viewBox={`0 0 ${width} ${height}`}
          className="h-52 w-full min-w-[420px]"
          role="img"
          aria-label={t('usage.chartLabel')}
        >
          {points.map((p, i) => {
            const x = i * slot + slot / 2
            const downH = (p.down / max) * chartH
            const upH = (p.up / max) * chartH
            const showLabel = i % Math.ceil(points.length / 8) === 0
            return (
              <g key={p.t}>
                <rect
                  x={x - barW - 1}
                  y={padTop + chartH - downH}
                  width={barW}
                  height={downH}
                  className="fill-brand"
                >
                  <title>
                    {label(p.t)} · {formatBytes(p.down, formatNumber)}
                  </title>
                </rect>
                <rect
                  x={x + 1}
                  y={padTop + chartH - upH}
                  width={barW}
                  height={upH}
                  className="fill-brand-strong"
                >
                  <title>
                    {label(p.t)} · {formatBytes(p.up, formatNumber)}
                  </title>
                </rect>
                {showLabel ? (
                  <text
                    x={x}
                    y={height - 8}
                    textAnchor="middle"
                    className="fill-current text-[9px] text-ink-muted"
                  >
                    {label(p.t)}
                  </text>
                ) : null}
              </g>
            )
          })}
          <line
            x1={0}
            y1={padTop + chartH}
            x2={width}
            y2={padTop + chartH}
            className="stroke-surface-sunken"
            strokeWidth={1}
          />
        </svg>
      </ChartContainer>
    </div>
  )
}
