import { useFormatters, useT } from '@hikrad/shared'

import { useBranding } from '../../hooks/useBranding'

/** ISP header + generated-at, shown only in the print view (task 1). */
export function PrintHeader({ reportTitle }: { reportTitle: string }) {
  const branding = useBranding()
  const { formatDate } = useFormatters()
  const t = useT()
  return (
    <div data-testid="print-header" className="mb-4 hidden border-b border-ink pb-3 print:block">
      <div className="flex items-center justify-between">
        <span className="text-lg font-bold">{branding.name}</span>
        <span className="text-sm">{reportTitle}</span>
      </div>
      <p className="mt-1 text-xs text-ink-muted">
        {t('reports.generatedAt', {
          at: formatDate(new Date(), { dateStyle: 'medium', timeStyle: 'short' }),
        })}
      </p>
    </div>
  )
}
