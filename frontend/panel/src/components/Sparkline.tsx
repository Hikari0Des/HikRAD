import { ChartContainer } from '@hikrad/shared'

/**
 * Minimal dependency-free SVG sparkline (online-now trend on the dashboard).
 * Wrapped in <ChartContainer> so it stays LTR inside an RTL page (charts never
 * mirror). Purely presentational; the caller supplies the series.
 */
export function Sparkline({
  values,
  width = 120,
  height = 32,
  label,
}: {
  values: number[]
  width?: number
  height?: number
  label: string
}) {
  if (values.length < 2) return null
  const max = Math.max(...values, 1)
  const min = Math.min(...values, 0)
  const span = max - min || 1
  const step = width / (values.length - 1)
  const points = values
    .map((v, i) => `${(i * step).toFixed(1)},${(height - ((v - min) / span) * height).toFixed(1)}`)
    .join(' ')

  return (
    <ChartContainer>
      <svg
        width={width}
        height={height}
        viewBox={`0 0 ${width} ${height}`}
        role="img"
        aria-label={label}
        className="text-brand"
      >
        <polyline
          points={points}
          fill="none"
          stroke="currentColor"
          strokeWidth="1.5"
          strokeLinejoin="round"
          strokeLinecap="round"
        />
      </svg>
    </ChartContainer>
  )
}
