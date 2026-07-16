/**
 * @hikrad/shared — the frozen contract-C8 surface plus the shared utilities
 * every HikRAD UI consumes (see README.md for the rules).
 */

// i18n framework (contract C8)
export { I18nProvider, useLocale, useT } from './i18n/I18nProvider'
export type { I18nProviderProps, TFunction, TOptions } from './i18n/I18nProvider'
export { DIRS, INTL_LOCALE, intlLocale, isLocale, LOCALES } from './i18n/locales'
export type { Dir, Locale, Numerals } from './i18n/locales'

// Formatting (NFR-6.3)
export { formatDate, formatIQD, formatNumber } from './format/format'
export type { FormatDateOptions, FormatOptions } from './format/format'
export { useFormatters } from './format/useFormatters'

// Bidi / RTL utilities (NFR-6.2)
export { ChartContainer, Ltr } from './bidi/Ltr'

// Thin API client (contract C2)
export { ApiError, createApiClient } from './api/client'
export type { ApiClient, ApiClientOptions, FieldError, ListParams, Page } from './api/client'

// Shared UI primitives (import '@hikrad/shared/ui.css' once per app)
export { StatusBadge } from './ui/StatusBadge'
export type { SubscriberStatus } from './ui/StatusBadge'
export { QuotaBar } from './ui/QuotaBar'
export { EmptyState, ErrorState, LoadingState } from './ui/states'
export { IQDAmount } from './ui/IQDAmount'

// Theme preference (item 19): dark/light/system, applied as data-theme on <html>
export { initTheme, setThemePreference, THEME_PREFERENCES, useTheme } from './ui/theme'
export type { ThemePreference } from './ui/theme'
