import { normalizeRatePair, parseRateKbps, useFormatters, useT } from '@hikrad/shared'

import { TextInput } from './TextInput'

/**
 * Rate entry with unit prefixes (item 5): "10G" / "1M" / bare "22" (= kbit);
 * empty or 0 = unlimited. A live line under the input shows how the value is
 * understood ("= 10 Mbit/s", "Unlimited", or an error), so Sara never has to
 * know the kbps convention. `pair` mode accepts "up/down" (a single value
 * applies to both) for the rx/tx string fields (rate_override, throttle_rate).
 */
export function RateInput({
  id,
  value,
  onChange,
  pair = false,
  disabled,
}: {
  id?: string
  value: string
  onChange: (value: string) => void
  pair?: boolean
  disabled?: boolean
}) {
  const t = useT()
  const { formatNumber } = useFormatters()

  let preview: string
  let invalid = false
  if (pair) {
    const norm = normalizeRatePair(value)
    if (norm === null) {
      preview = t('rate.invalid')
      invalid = true
    } else if (norm === '') {
      preview = t('rate.inherit')
    } else {
      preview = `= ${norm}`
    }
  } else {
    const kbps = parseRateKbps(value)
    if (kbps === null) {
      preview = t('rate.invalid')
      invalid = true
    } else if (kbps === 0) {
      preview = t('rate.unlimited')
    } else if (kbps % 1024 === 0) {
      preview = t('rate.mbit', { n: formatNumber(kbps / 1024) })
    } else {
      preview = t('rate.kbit', { n: formatNumber(kbps) })
    }
  }

  return (
    <div>
      <TextInput
        id={id}
        dir="ltr"
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder={pair ? t('rate.pairPlaceholder') : t('rate.placeholder')}
        disabled={disabled}
        aria-invalid={invalid || undefined}
      />
      <p className={`mt-1 text-xs ${invalid ? 'text-danger' : 'text-ink-muted'}`} dir="ltr">
        {preview}
      </p>
    </div>
  )
}
