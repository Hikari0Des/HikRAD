import { useT } from '@hikrad/shared'

import { Button } from '../../components/Button'
import { TextInput } from '../../components/form'
import type { ReportRangeState } from './useReportRange'

const PRESETS = ['today', 'week', 'month'] as const

/** Date-range presets + custom pickers (task 1), state lives in the URL. */
export function DateRangeFilter({ range }: { range: ReportRangeState }) {
  const t = useT()
  return (
    <div className="mb-4 flex flex-wrap items-center gap-2 print:hidden">
      <div className="flex overflow-hidden rounded-md border border-surface-sunken">
        {PRESETS.map((p) => (
          <button
            key={p}
            type="button"
            onClick={() => range.setPreset(p)}
            className={`px-3 py-1.5 text-xs font-medium ${
              range.preset === p
                ? 'bg-brand text-ink-inverse'
                : 'bg-surface text-ink hover:bg-surface-sunken'
            }`}
          >
            {t(`reports.range.${p}`)}
          </button>
        ))}
      </div>
      <TextInput
        type="date"
        aria-label={t('reports.range.from')}
        value={range.fromDate}
        onChange={(e) => range.setCustom(e.target.value, range.toDate)}
        className="w-auto"
      />
      <span className="text-ink-muted">–</span>
      <TextInput
        type="date"
        aria-label={t('reports.range.to')}
        value={range.toDate}
        onChange={(e) => range.setCustom(range.fromDate, e.target.value)}
        className="w-auto"
      />
      <Button size="sm" variant="ghost" onClick={() => window.print()} title={t('reports.print')}>
        {t('reports.print')}
      </Button>
    </div>
  )
}
