import { useLocale, useT, LOCALES } from '@hikrad/shared'

import { useAuth } from '../auth/AuthContext'
import { setLanguage } from '../api/me'

/**
 * Segmented en/ar/ku switcher (task brief; required on the login page).
 * Switching is instant local state regardless of auth; when a subscriber is
 * signed in, the choice is also persisted server-side (FR-43, task 4) —
 * best-effort, never blocking the UI switch on the network.
 */
export function LanguageSwitcher() {
  const { locale, setLocale } = useLocale()
  const { subscriber } = useAuth()
  const t = useT()
  return (
    <div role="group" aria-label={t('portal.login.language')} className="inline-flex gap-1">
      {LOCALES.map((code) => (
        <button
          key={code}
          type="button"
          onClick={() => {
            setLocale(code)
            if (subscriber) void setLanguage(code).catch(() => {})
          }}
          aria-pressed={locale === code}
          className={`rounded-md px-3 py-1 text-sm transition-colors ${
            locale === code
              ? 'bg-brand text-ink-inverse'
              : 'bg-surface-sunken text-ink-muted hover:text-ink'
          }`}
        >
          {t(`common.languages.${code}`)}
        </button>
      ))}
    </div>
  )
}
