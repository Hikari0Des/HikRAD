import { useFormatters } from '../format/useFormatters'

/**
 * IQD amount, formatted for the active locale/numerals and bidi-isolated
 * (<bdi>) so the currency symbol and digits never reorder against the
 * surrounding sentence. Not <Ltr>: the Arabic rendering ("د.ع ١٥٬٠٠٠") is
 * legitimately RTL — it needs isolation, not a forced direction.
 */
export function IQDAmount({ amount, className }: { amount: number; className?: string }) {
  const { formatIQD } = useFormatters()
  return <bdi className={`hk-iqd${className ? ` ${className}` : ''}`}>{formatIQD(amount)}</bdi>
}
