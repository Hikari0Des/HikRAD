/** Contract C8: the three product locales. `ku` is Kurdish Sorani. */
export type Locale = 'en' | 'ar' | 'ku'
export type Dir = 'ltr' | 'rtl'

export const LOCALES: readonly Locale[] = ['en', 'ar', 'ku']

export const DIRS: Record<Locale, Dir> = { en: 'ltr', ar: 'rtl', ku: 'rtl' }

/**
 * BCP 47 tags handed to Intl. Kurdish Sorani is `ckb`; environments without
 * ckb CLDR data fall back gracefully inside the formatters.
 */
export const INTL_LOCALE: Record<Locale, string> = { en: 'en-US', ar: 'ar-IQ', ku: 'ckb-IQ' }

export function isLocale(value: unknown): value is Locale {
  return typeof value === 'string' && (LOCALES as readonly string[]).includes(value)
}

/**
 * Numeral-shape preference (NFR-6.3): 'auto' uses the locale's default digits
 * (Eastern Arabic for ar-IQ), 'arab' forces Eastern Arabic (٠١٢…), 'latn'
 * forces Western digits (012…). Later phases seed this from server settings
 * (FR-53.2) via <I18nProvider defaultNumerals>.
 */
export type Numerals = 'auto' | 'latn' | 'arab'

export function isNumerals(value: unknown): value is Numerals {
  return value === 'auto' || value === 'latn' || value === 'arab'
}

/** Resolve the Intl locale tag, applying a forced numbering system if any. */
export function intlLocale(locale: Locale, numerals: Numerals = 'auto'): string {
  const base = INTL_LOCALE[locale]
  return numerals === 'auto' ? base : `${base}-u-nu-${numerals}`
}
