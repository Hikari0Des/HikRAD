import { useMemo } from 'react'

import { useLocale } from '../i18n/I18nProvider'
import {
  formatDate,
  formatIQD,
  formatMoney,
  formatNumber,
  type CurrencyCode,
  type FormatDateOptions,
  type FormatOptions,
} from './format'

/**
 * Formatters pre-bound to the active locale and numeral preference from
 * <I18nProvider> — the usual way components format values (the pure functions
 * in format.ts remain for non-React code and explicit locales).
 */
export function useFormatters() {
  const { locale, numerals } = useLocale()
  return useMemo(
    () => ({
      formatNumber: (value: number, opts: FormatOptions & Intl.NumberFormatOptions = {}) =>
        formatNumber(value, locale, { numerals, ...opts }),
      formatIQD: (amount: number, opts: FormatOptions = {}) =>
        formatIQD(amount, locale, { numerals, ...opts }),
      formatMoney: (amount: number, currency: CurrencyCode, opts: FormatOptions = {}) =>
        formatMoney(amount, currency, locale, { numerals, ...opts }),
      formatDate: (date: Date | string, opts: FormatDateOptions = {}) =>
        formatDate(date, locale, { numerals, ...opts }),
    }),
    [locale, numerals],
  )
}
