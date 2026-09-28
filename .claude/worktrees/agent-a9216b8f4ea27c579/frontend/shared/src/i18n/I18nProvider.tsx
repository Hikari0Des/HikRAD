/**
 * Contract C8: I18nProvider / useT() / useLocale().
 *
 * - Locale detection: stored preference → browser language → 'en'.
 * - Fallback chain: locale → en → key. NEVER ku→ar — Kurdish Sorani is a
 *   distinct locale; missing ku strings fall back to English and are tracked
 *   by `npm run i18n:check` (they must be 0 for the v1 cut).
 * - Sets lang/dir on <html>; switching locale is pure React state — no
 *   reload, so app state survives the switch.
 */
import { createContext, useContext, useEffect, useMemo, useState, type ReactNode } from 'react'

import { formatMessage, type IcuFormatOptions } from './icu'
import { DIRS, isLocale, isNumerals, type Dir, type Locale, type Numerals } from './locales'
import { MESSAGES, resolveMessage } from './messages'

const LOCALE_STORAGE_KEY = 'hikrad.locale'
const NUMERALS_STORAGE_KEY = 'hikrad.numerals'

function detectLocale(fallback: Locale): Locale {
  try {
    const stored = window.localStorage.getItem(LOCALE_STORAGE_KEY)
    if (isLocale(stored)) return stored
  } catch {
    // storage unavailable (private mode etc.) — fall through
  }
  const nav = typeof navigator !== 'undefined' ? navigator.language.slice(0, 2) : ''
  return isLocale(nav) ? nav : fallback
}

function detectNumerals(fallback: Numerals): Numerals {
  try {
    const stored = window.localStorage.getItem(NUMERALS_STORAGE_KEY)
    if (isNumerals(stored)) return stored
  } catch {
    // fall through
  }
  return fallback
}

interface I18nContextValue {
  locale: Locale
  dir: Dir
  setLocale: (locale: Locale) => void
  /** Numeral-shape preference (NFR-6.3); 'auto' = the locale's default digits. */
  numerals: Numerals
  setNumerals: (numerals: Numerals) => void
}

const I18nContext = createContext<I18nContextValue | null>(null)

export interface I18nProviderProps {
  children: ReactNode
  /** Used when neither a stored preference nor the browser language decides. */
  defaultLocale?: Locale
  /** Later phases seed this from server settings (FR-53.2). */
  defaultNumerals?: Numerals
}

export function I18nProvider({
  children,
  defaultLocale = 'en',
  defaultNumerals = 'auto',
}: I18nProviderProps) {
  const [locale, setLocaleState] = useState<Locale>(() => detectLocale(defaultLocale))
  const [numerals, setNumeralsState] = useState<Numerals>(() => detectNumerals(defaultNumerals))

  useEffect(() => {
    document.documentElement.lang = locale
    document.documentElement.dir = DIRS[locale]
  }, [locale])

  const value = useMemo<I18nContextValue>(
    () => ({
      locale,
      dir: DIRS[locale],
      numerals,
      setLocale: (next) => {
        try {
          window.localStorage.setItem(LOCALE_STORAGE_KEY, next)
        } catch {
          // best-effort persistence only
        }
        setLocaleState(next)
      },
      setNumerals: (next) => {
        try {
          window.localStorage.setItem(NUMERALS_STORAGE_KEY, next)
        } catch {
          // best-effort persistence only
        }
        setNumeralsState(next)
      },
    }),
    [locale, numerals],
  )

  return <I18nContext.Provider value={value}>{children}</I18nContext.Provider>
}

function useI18n(): I18nContextValue {
  const ctx = useContext(I18nContext)
  if (!ctx) throw new Error('useT/useLocale must be used inside <I18nProvider>')
  return ctx
}

/** Contract C8: `useLocale()` → `{ locale, dir, setLocale, … }`, dir auto. */
export function useLocale() {
  return useI18n()
}

export type TOptions = Pick<IcuFormatOptions, 'isolate'>

export type TFunction = (
  key: string,
  vars?: Record<string, string | number>,
  opts?: TOptions,
) => string

/**
 * Contract C8: `useT()` with namespaced `"module.key"` keys, `{name}`
 * interpolation and ICU-style plurals. `opts.isolate` bidi-isolates
 * interpolated values for mixed RTL/LTR sentences.
 */
export function useT(): TFunction {
  const { locale, numerals } = useI18n()
  return useMemo<TFunction>(
    () => (key, vars, opts) => {
      const raw = resolveMessage(MESSAGES, locale, key)
      return formatMessage(raw, vars, locale, { numerals, isolate: opts?.isolate })
    },
    [locale, numerals],
  )
}
