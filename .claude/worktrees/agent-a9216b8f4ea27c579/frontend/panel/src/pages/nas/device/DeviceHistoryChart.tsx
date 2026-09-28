import { useMemo } from 'react'

import { ChartContainer, useFormatters, useT } from '@hikrad/shared'

import type { DeviceHistoryPoint } from '../../../api/types'

const WIDTH = 640

/** Builds an SVG polyline path for `values` scaled into [0, max] over a chart
 * area `chartH` tall, padded `padTop` from the viewBox top. */
function buildPath(values: number[], max: number, chartH: number, padTop: number): string {
  if (values.length < 2) return ''
  const step = WIDTH / (values.length - 1)
  return values
    .map((v, i) => {
      const x = i * step
      const y = padTop + chartH - (Math.max(0, Math.min(v, max)) / max) * chartH
      return `${i === 0 ? 'M' : 'L'}${x.toFixed(1)},${y.toFixed(1)}`
    })
    .join(' ')
}

/**
 * CPU/memory/session-count history (FR-141 Overview tab). v4-1's chart-N
 * palette tokens only (no raw color, phase brief binding rule) — CPU is
 * chart-1, memory chart-2, session count chart-3. Two separate scales: CPU
 * and memory are both 0-100%, session count is its own unit and gets its own
 * small chart beneath rather than a distorted shared axis.
 */
export function DeviceHistoryChart({ points }: { points: DeviceHistoryPoint[] }) {
  const t = useT()
  const { formatDate } = useFormatters()

  const maxSessions = useMemo(() => Math.max(1, ...points.map((p) => p.session_count)), [points])

  if (points.length < 2) {
    return <p className="py-6 text-center text-sm text-ink-muted">{t('nas.device.noHistory')}</p>
  }

  const pctHeight = 128
  const pctPad = 6
  const cpuPath = buildPath(
    points.map((p) => p.cpu_percent),
    100,
    pctHeight,
    pctPad,
  )
  const memPath = buildPath(
    points.map((p) => p.mem_percent),
    100,
    pctHeight,
    pctPad,
  )

  const sessHeight = 48
  const sessPad = 6
  const sessionPath = buildPath(
    points.map((p) => p.session_count),
    maxSessions,
    sessHeight,
    sessPad,
  )

  const first = points[0].time
  const last = points[points.length - 1].time

  return (
    <div className="space-y-4">
      <div>
        <div className="mb-1 flex items-center gap-4 text-xs text-ink-muted">
          <Legend swatch="bg-chart-1" label={t('nas.device.overview.cpu')} />
          <Legend swatch="bg-chart-2" label={t('nas.device.overview.mem')} />
        </div>
        <ChartContainer className="w-full overflow-x-auto">
          <svg
            viewBox={`0 0 ${WIDTH} ${pctHeight + pctPad * 2}`}
            className="h-36 w-full min-w-[420px]"
            role="img"
            aria-label={t('nas.device.overview.cpuMemChart')}
          >
            <line
              x1={0}
              y1={pctPad + pctHeight}
              x2={WIDTH}
              y2={pctPad + pctHeight}
              className="stroke-border"
              strokeWidth={1}
            />
            <path d={cpuPath} fill="none" className="stroke-chart-1" strokeWidth={1.75} />
            <path d={memPath} fill="none" className="stroke-chart-2" strokeWidth={1.75} />
          </svg>
        </ChartContainer>
      </div>
      <div>
        <div className="mb-1 flex items-center gap-4 text-xs text-ink-muted">
          <Legend swatch="bg-chart-3" label={t('nas.device.overview.sessions')} />
        </div>
        <ChartContainer className="w-full overflow-x-auto">
          <svg
            viewBox={`0 0 ${WIDTH} ${sessHeight + sessPad * 2}`}
            className="h-16 w-full min-w-[420px]"
            role="img"
            aria-label={t('nas.device.overview.sessionsChart')}
          >
            <path d={sessionPath} fill="none" className="stroke-chart-3" strokeWidth={1.75} />
          </svg>
        </ChartContainer>
      </div>
      <div className="flex justify-between text-xs text-ink-muted">
        <span>{formatDate(first)}</span>
        <span>{formatDate(last)}</span>
      </div>
    </div>
  )
}

function Legend({ swatch, label }: { swatch: string; label: string }) {
  return (
    <span className="flex items-center gap-1.5">
      <span aria-hidden="true" className={`inline-block h-2.5 w-2.5 rounded-sm ${swatch}`} />
      {label}
    </span>
  )
}
