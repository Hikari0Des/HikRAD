import { ChartContainer } from '@hikrad/shared'

/**
 * Dependency-free multi-series line chart (v4-12 trend lines: active
 * subscribers / online sessions / expired subscribers). Same construction as
 * ../Sparkline.tsx (this codebase's only precedent for a chart), extended for
 * more than one series: a fixed-order categorical palette (chart-1/2/3,
 * dataviz skill's "assign categorical hues in fixed order, never cycled"),
 * a legend (required for >= 2 series), and a per-point <title> tooltip
 * (dependency-free hover layer — no charting library in this project).
 */
export interface LineSeries {
  key: string
  label: string
  /** Tailwind text-color class, e.g. "text-chart-1" — chosen by the caller in
   * fixed series order, never cycled per-render. */
  colorClass: string
  values: number[]
}

export function MultiLineChart({
  series,
  labels,
  width = 640,
  height = 200,
}: {
  series: LineSeries[]
  /** X-axis labels (dates), same length as every series' values. */
  labels: string[]
  width?: number
  height?: number
}) {
  const n = labels.length
  if (n < 2) return null
  const allValues = series.flatMap((s) => s.values)
  const max = Math.max(...allValues, 1)
  const step = width / (n - 1)
  const padTop = 8

  function points(values: number[]): string {
    return values
      .map((v, i) => {
        const x = i * step
        const y = padTop + (height - padTop) * (1 - v / max)
        return `${x.toFixed(1)},${y.toFixed(1)}`
      })
      .join(' ')
  }

  return (
    <ChartContainer>
      <svg
        width="100%"
        viewBox={`0 0 ${width} ${height}`}
        role="img"
        aria-label={series.map((s) => s.label).join(', ')}
      >
        {/* Recessive baseline, per the dataviz skill's "recessive grid/axes". */}
        <line
          x1={0}
          y1={height - 1}
          x2={width}
          y2={height - 1}
          className="stroke-border"
          strokeWidth={1}
        />
        {series.map((s) => (
          <g key={s.key} className={s.colorClass}>
            <polyline
              points={points(s.values)}
              fill="none"
              stroke="currentColor"
              strokeWidth={2}
              strokeLinejoin="round"
              strokeLinecap="round"
            />
            {s.values.map((v, i) => {
              const x = i * step
              const y = padTop + (height - padTop) * (1 - v / max)
              return (
                <circle key={i} cx={x} cy={y} r={2.5} fill="currentColor">
                  <title>
                    {labels[i]}: {s.label} = {v}
                  </title>
                </circle>
              )
            })}
          </g>
        ))}
      </svg>
      <ul className="mt-2 flex flex-wrap gap-x-4 gap-y-1 text-xs">
        {series.map((s) => (
          <li key={s.key} className="flex items-center gap-1.5">
            <span className={`inline-block h-2 w-2 rounded-full ${s.colorClass} bg-current`} />
            <span className="text-ink-muted">{s.label}</span>
          </li>
        ))}
      </ul>
    </ChartContainer>
  )
}
