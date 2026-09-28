import { THEME_PREFERENCES, useT, useTheme } from '@hikrad/shared'

/**
 * Cycle-button theme picker (item 19): tapping steps light → dark → system.
 * A single compact control (not a segmented group) because the portal header
 * is phone-width (Noor) and already carries the language switcher.
 */
export function ThemeSwitcher() {
  const { theme, setTheme } = useTheme()
  const t = useT()

  function next() {
    const i = THEME_PREFERENCES.indexOf(theme)
    setTheme(THEME_PREFERENCES[(i + 1) % THEME_PREFERENCES.length])
  }

  return (
    <button
      type="button"
      onClick={next}
      aria-label={t('common.theme.label')}
      title={t(`common.theme.${theme}`)}
      className="rounded-md bg-surface-sunken px-3 py-1 text-sm text-ink-muted hover:text-ink"
    >
      {t(`common.theme.${theme}`)}
    </button>
  )
}
