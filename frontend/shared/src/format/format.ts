/**
 * Locale-aware formatting (contract C8 / NFR-6.3): IQD currency, dates in the
 * product timezone (Asia/Baghdad), and numbers with an Eastern-Arabic numeral
 * option. Server-settings integration (FR-53.2) arrives in a later phase by
 * seeding <I18nProvider defaultNumerals>; these functions stay pure.
 */
import { intlLocale, type Locale, type Numerals } from '../i18n/locales'

export interface FormatOptions {
  numerals?: Numerals
}

/** Plain number with locale digits. `numerals: 'arab'` forces ٠١٢…, 'latn' 012… */
export function formatNumber(
  value: number,
  locale: Locale = 'en',
  opts: FormatOptions & Intl.NumberFormatOptions = {},
): string {
  const { numerals = 'auto', ...intlOpts } = opts
  try {
    return new Intl.NumberFormat(intlLocale(locale, numerals), intlOpts).format(value)
  } catch {
    return String(value)
  }
}

/** Contract C8: IQD currency formatting (IQD has no minor unit in practice). */
export function formatIQD(amount: number, locale: Locale = 'en', opts: FormatOptions = {}): string {
  const { numerals = 'auto' } = opts
  try {
    return new Intl.NumberFormat(intlLocale(locale, numerals), {
      style: 'currency',
      currency: 'IQD',
      maximumFractionDigits: 0,
    }).format(amount)
  } catch {
    return `${amount} IQD`
  }
}

export interface FormatDateOptions extends FormatOptions {
  dateStyle?: Intl.DateTimeFormatOptions['dateStyle']
  timeStyle?: Intl.DateTimeFormatOptions['timeStyle']
  timeZone?: string
}

/** Contract C8: date formatting, product timezone Asia/Baghdad (PRD). */
export function formatDate(
  date: Date | string,
  locale: Locale = 'en',
  opts: FormatDateOptions = {},
): string {
  const d = typeof date === 'string' ? new Date(date) : date
  const { numerals = 'auto', dateStyle = 'medium', timeStyle = 'short', timeZone } = opts
  try {
    return new Intl.DateTimeFormat(intlLocale(locale, numerals), {
      dateStyle,
      timeStyle,
      timeZone: timeZone ?? 'Asia/Baghdad',
    }).format(d)
  } catch {
    return d.toISOString()
  }
}
