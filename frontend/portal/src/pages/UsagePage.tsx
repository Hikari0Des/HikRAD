import { ChartContainer, Ltr, QuotaBar, useT } from '@hikrad/shared'

// Placeholder daily-usage bars (GB) until real charts arrive in Phase 4.
const SAMPLE_BARS = [3, 5, 2, 7, 4, 6, 8]

/**
 * Usage stub. Demonstrates the frozen bidi rules: the chart stays LTR inside
 * RTL pages (ChartContainer), usernames stay LTR (<Ltr>), and interpolated
 * values in the mixed sentence are bidi-isolated so "4.2 GB" never reorders
 * in Arabic (task edge case).
 */
export function UsagePage() {
  const t = useT()

  return (
    <section className="flex flex-col gap-4">
      <h1 className="text-lg font-semibold">{t('portal.usage.title')}</h1>

      <div className="flex flex-col gap-2 rounded-xl bg-surface-raised p-4 shadow-sm">
        <h2 className="text-sm font-semibold">{t('portal.usage.chartTitle')}</h2>
        <ChartContainer className="flex h-24 items-end gap-1">
          {SAMPLE_BARS.map((value, i) => (
            <div
              key={i}
              className="flex-1 rounded-t bg-brand/70"
              style={{ blockSize: `${value * 12}%` }}
            />
          ))}
        </ChartContainer>
        <p className="text-xs text-ink-muted">{t('portal.usage.chartPlaceholder')}</p>
      </div>

      <div className="flex flex-col gap-2 rounded-xl bg-surface-raised p-4 shadow-sm text-sm">
        <p>{t('portal.usage.mixedSample', { username: 'noor01', gb: '4.2' }, { isolate: true })}</p>
        <p>{t('portal.usage.daysLeft', { count: 12 })}</p>
        <p className="text-ink-muted">
          <Ltr className="font-mono">noor01</Ltr>
        </p>
      </div>

      <QuotaBar used={92} total={100} />
    </section>
  )
}
