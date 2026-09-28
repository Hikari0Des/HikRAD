import { useLocale, useT, useTheme, THEME_PREFERENCES, type Locale } from '@hikrad/shared'

/**
 * One-press top-bar shortcuts for the two settings people flip most (theme,
 * language) so neither hides behind the user menu. Each press advances to the
 * next value; the full pickers remain in UserMenu/preferences for direct
 * selection.
 */

const LOCALES: readonly Locale[] = ['en', 'ar', 'ku']

export function ThemeToggle() {
  const { theme, setTheme } = useTheme()
  const t = useT()

  const idx = THEME_PREFERENCES.indexOf(theme)
  const next = THEME_PREFERENCES[(idx + 1) % THEME_PREFERENCES.length]

  return (
    <button
      type="button"
      onClick={() => setTheme(next)}
      aria-label={t('common.theme.label')}
      title={t(`common.theme.${theme}`)}
      className="rounded-md p-2 text-ink-muted hover:bg-surface-sunken hover:text-ink"
    >
      <ThemeIcon theme={theme} />
    </button>
  )
}

export function LocaleToggle() {
  const { locale, setLocale } = useLocale()
  const t = useT()

  const idx = LOCALES.indexOf(locale)
  const next = LOCALES[(idx + 1) % LOCALES.length]

  return (
    <button
      type="button"
      onClick={() => setLocale(next)}
      aria-label={t('user.language')}
      title={t(`languages.${locale}`)}
      className="rounded-md px-2 py-1.5 text-xs font-semibold uppercase text-ink-muted hover:bg-surface-sunken hover:text-ink"
    >
      <span dir="ltr">{locale}</span>
    </button>
  )
}

function ThemeIcon({ theme }: { theme: string }) {
  // sun / moon / half-disc (system follows the OS)
  if (theme === 'light') {
    return (
      <svg
        aria-hidden="true"
        width="18"
        height="18"
        viewBox="0 0 20 20"
        fill="none"
        stroke="currentColor"
        strokeWidth="1.6"
        strokeLinecap="round"
      >
        <circle cx="10" cy="10" r="4" />
        <path d="M10 1.5v2M10 16.5v2M1.5 10h2M16.5 10h2M4 4l1.4 1.4M14.6 14.6 16 16M16 4l-1.4 1.4M5.4 14.6 4 16" />
      </svg>
    )
  }
  if (theme === 'dark') {
    return (
      <svg
        aria-hidden="true"
        width="18"
        height="18"
        viewBox="0 0 20 20"
        fill="none"
        stroke="currentColor"
        strokeWidth="1.6"
        strokeLinecap="round"
        strokeLinejoin="round"
      >
        <path d="M16.5 12.5A7 7 0 0 1 7.5 3.5a7 7 0 1 0 9 9Z" />
      </svg>
    )
  }
  return (
    <svg
      aria-hidden="true"
      width="18"
      height="18"
      viewBox="0 0 20 20"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.6"
    >
      <circle cx="10" cy="10" r="7" />
      <path d="M10 3a7 7 0 0 1 0 14Z" fill="currentColor" stroke="none" />
    </svg>
  )
}
