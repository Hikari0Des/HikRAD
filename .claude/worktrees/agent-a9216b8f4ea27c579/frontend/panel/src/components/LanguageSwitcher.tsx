import { useLocale, useT, type Locale } from '@hikrad/shared'

const LOCALES: readonly Locale[] = ['en', 'ar', 'ku']

/** Segmented en/ar/ku switcher (login page, smoke page; the top bar uses menu items). */
export function LanguageSwitcher() {
  const { locale, setLocale } = useLocale()
  const t = useT()
  return (
    <div role="group" aria-label={t('user.language')} className="inline-flex gap-1">
      {LOCALES.map((code) => (
        <button
          key={code}
          type="button"
          onClick={() => setLocale(code)}
          aria-pressed={locale === code}
          className={`rounded-md px-3 py-1 text-sm transition-colors ${
            locale === code
              ? 'bg-brand text-ink-inverse'
              : 'bg-surface-sunken text-ink-muted hover:text-ink'
          }`}
        >
          {t(`languages.${code}`)}
        </button>
      ))}
    </div>
  )
}
