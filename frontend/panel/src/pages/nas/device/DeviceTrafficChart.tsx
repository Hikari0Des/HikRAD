import { useMemo } from 'react'

import { ChartContainer, useFormatters, useT } from '@hikrad/shared'

import { formatBps } from '../../../lib/units'
import type { DeviceTrafficPoint } from '../../../api/types'

const WIDTH = 640
const HEIGHT = 120
const PAD = 6

/**
 * Per-interface rx/tx throughput history (FR-141 Interfaces tab), one
 * interface at a time (?iface= selection). chart-4 (download) / chart-5
 * (upload) — distinct from the Overview chart's chart-1/2/3 so the two tabs
 * never collide on a series color.
 */
export function DeviceTrafficChart({ points }: { points: DeviceTrafficPoint[] }) {
  const t = useT()
  const { formatNumber, formatDate } = useFormatters()

  const max = useMemo(
    () => Math.max(1, ...points.map((p) => Math.max(p.rx_bps, p.tx_bps))),
    [points],
  )

  if (points.length < 2) {
    return <p className="py-6 text-center text-sm text-ink-muted">{t('nas.device.noHistory')}</p>
  }

  const chartH = HEIGHT - PAD * 2
  const step = WIDTH / (points.length - 1)
  const path = (values: number[]) =>
    values
      .map((v, i) => {
        const x = i * step
        const y = PAD + chartH - (Math.max(0, v) / max) * chartH
        return `${i === 0 ? 'M' : 'L'}${x.toFixed(1)},${y.toFixed(1)}`
      })
      .join(' ')

  const rxPath = path(points.map((p) => p.rx_bps))
  const txPath = path(points.map((p) => p.tx_bps))
  const first = points[0].time
  const last = points[points.length - 1].time

  return (
    <div>
      <div className="mb-1 flex items-center gap-4 text-xs text-ink-muted">
        <span className="flex items-center gap-1.5">
          <span aria-hidden="true" className="inline-block h-2.5 w-2.5 rounded-sm bg-chart-4" />
          {t('nas.device.interfaces.download')} · {formatBps(max, formatNumber)}{' '}
          {t('nas.device.interfaces.peak')}
        </span>
        <span className="flex items-center gap-1.5">
          <span aria-hidden="true" className="inline-block h-2.5 w-2.5 rounded-sm bg-chart-5" />
          {t('nas.device.interfaces.upload')}
        </span>
      </div>
      <ChartContainer className="w-full overflow-x-auto">
        <svg
          viewBox={`0 0 ${WIDTH} ${HEIGHT}`}
          className="h-32 w-full min-w-[420px]"
          role="img"
          aria-label={t('nas.device.interfaces.trafficChart')}
        >
          <line
            x1={0}
            y1={PAD + chartH}
            x2={WIDTH}
            y2={PAD + chartH}
            className="stroke-border"
            strokeWidth={1}
          />
          <path d={rxPath} fill="none" className="stroke-chart-4" strokeWidth={1.75} />
          <path d={txPath} fill="none" className="stroke-chart-5" strokeWidth={1.75} />
        </svg>
      </ChartContainer>
      <div className="mt-1 flex justify-between text-xs text-ink-muted">
        <span>{formatDate(first)}</span>
        <span>{formatDate(last)}</span>
      </div>
    </div>
  )
}
