import { useFormatters } from '../format/useFormatters'
import type { CurrencyCode } from '../format/format'

/**
 * Amount formatted for the active locale/numerals and bidi-isolated (<bdi>)
 * so the currency symbol and digits never reorder against the surrounding
 * sentence. Not <Ltr>: the Arabic rendering ("د.ع ١٥٬٠٠٠") is legitimately
 * RTL — it needs isolation, not a forced direction.
 *
 * `currency` defaults to 'IQD' (v2 phase 4, FR-70.1) so every pre-existing
 * call site keeps compiling unchanged; pass the row's real currency once its
 * data source actually carries one.
 */
export function IQDAmount({
  amount,
  currency = 'IQD',
  className,
}: {
  amount: number
  currency?: CurrencyCode
  className?: string
}) {
  const { formatMoney } = useFormatters()
  return (
    <bdi className={`hk-iqd${className ? ` ${className}` : ''}`}>
      {formatMoney(amount, currency)}
    </bdi>
  )
}
