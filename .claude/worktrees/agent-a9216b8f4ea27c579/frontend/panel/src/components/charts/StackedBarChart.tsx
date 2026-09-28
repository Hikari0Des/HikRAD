import { ChartContainer } from '@hikrad/shared'

/**
 * Dependency-free stacked bar chart (v4-12 daily traffic: upload vs
 * download). Two segments per bar with a 2px surface gap between them (the
 * dataviz skill's stacked-segment spacer rule), a fixed-order two-series
 * legend, and a per-bar <title> tooltip giving both segments plus the
 * combined total (FR-173's own requirement, not just a nice-to-have).
 */
export interface StackedBarPoint {
  label: string
  a: number
  b: number
}

export function StackedBarChart({
  points,
  aLabel,
  bLabel,
  totalLabel,
  formatValue,
  width = 640,
  height = 200,
}: {
  points: StackedBarPoint[]
  aLabel: string
  bLabel: string
  totalLabel: string
  formatValue: (v: number) => string
  width?: number
  height?: number
}) {
  if (points.length === 0) return null
  const max = Math.max(...points.map((p) => p.a + p.b), 1)
  const gap = 4
  const barWidth = Math.max((width - gap * (points.length - 1)) / points.length, 1)
  const padTop = 8
  const usableHeight = height - padTop

  return (
    <ChartContainer>
      <svg
        width="100%"
        viewBox={`0 0 ${width} ${height}`}
        role="img"
        aria-label={`${aLabel}, ${bLabel}`}
      >
        <line
          x1={0}
          y1={height - 1}
          x2={width}
          y2={height - 1}
          className="stroke-border"
          strokeWidth={1}
        />
        {points.map((p, i) => {
          const x = i * (barWidth + gap)
          const aH = (p.a / max) * usableHeight
          const bH = (p.b / max) * usableHeight
          // 2px surface-colored gap between the two stacked segments.
          const segGap = p.a > 0 && p.b > 0 ? 2 : 0
          const bY = height - bH
          const aY = bY - segGap - aH
          const tooltip = [
            `${p.label}:`,
            `${aLabel} ${formatValue(p.a)},`,
            `${bLabel} ${formatValue(p.b)},`,
            `${totalLabel} ${formatValue(p.a + p.b)}`,
          ].join(' ')
          return (
            <g key={p.label}>
              {p.b > 0 && (
                <rect x={x} y={bY} width={barWidth} height={bH} rx={2} className="fill-chart-2">
                  <title>{tooltip}</title>
                </rect>
              )}
              {p.a > 0 && (
                <rect x={x} y={aY} width={barWidth} height={aH} rx={2} className="fill-chart-1">
                  <title>{tooltip}</title>
                </rect>
              )}
            </g>
          )
        })}
      </svg>
      <ul className="mt-2 flex flex-wrap gap-x-4 gap-y-1 text-xs">
        <li className="flex items-center gap-1.5">
          <span className="inline-block h-2 w-2 rounded-full bg-chart-1" />
          <span className="text-ink-muted">{aLabel}</span>
        </li>
        <li className="flex items-center gap-1.5">
          <span className="inline-block h-2 w-2 rounded-full bg-chart-2" />
          <span className="text-ink-muted">{bLabel}</span>
        </li>
      </ul>
    </ChartContainer>
  )
}
