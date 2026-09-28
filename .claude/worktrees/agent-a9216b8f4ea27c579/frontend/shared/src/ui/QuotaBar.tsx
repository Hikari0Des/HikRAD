import { useFormatters } from '../format/useFormatters'
import { useT } from '../i18n/I18nProvider'

/**
 * Quota progress bar. `used`/`total` are in the same unit; the caller formats
 * the human-readable amounts (they are unit-specific), the bar formats the
 * percentage with locale digits. Fills from the inline start, so it mirrors
 * under RTL automatically.
 */
export function QuotaBar({
  used,
  total,
  usedLabel,
  totalLabel,
  warnAt = 0.9,
  className,
}: {
  used: number
  total: number
  /** Pre-formatted amount strings shown under the bar (optional). */
  usedLabel?: string
  totalLabel?: string
  /** Fraction (0–1) past which the bar switches to the danger color. */
  warnAt?: number
  className?: string
}) {
  const t = useT()
  const { formatNumber } = useFormatters()
  const fraction = total > 0 ? Math.min(Math.max(used / total, 0), 1) : 0
  const percentText = formatNumber(fraction, { style: 'percent', maximumFractionDigits: 0 })
  const warn = fraction >= warnAt

  return (
    <div className={`hk-quota${warn ? ' hk-quota--warn' : ''}${className ? ` ${className}` : ''}`}>
      <div
        className="hk-quota__track"
        role="progressbar"
        aria-label={t('common.quota.label')}
        aria-valuemin={0}
        aria-valuemax={total}
        aria-valuenow={used}
        aria-valuetext={percentText}
      >
        <div className="hk-quota__fill" style={{ inlineSize: `${fraction * 100}%` }} />
      </div>
      <div className="hk-quota__meta">
        <span>
          {usedLabel !== undefined && totalLabel !== undefined
            ? t('common.quota.of', { used: usedLabel, total: totalLabel }, { isolate: true })
            : t('common.quota.label')}
        </span>
        <span>{percentText}</span>
      </div>
    </div>
  )
}
