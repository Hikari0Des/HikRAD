import { useT } from '@hikrad/shared'

import { TextInput } from '../../components/form'
import type { ReportRangeState } from './useReportRange'

const PRESETS = ['7d', '14d', '30d', '90d'] as const

/** FR-173's own date presets (7/14/30/90d) + custom, distinct from the
 * existing today/week/month filter the other 5 reports use. */
export function TrendRangeFilter({ range }: { range: ReportRangeState }) {
  const t = useT()
  return (
    <div className="mb-4 flex flex-wrap items-center gap-2 print:hidden">
      <div className="flex overflow-hidden rounded-md border border-border">
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
    </div>
  )
}
