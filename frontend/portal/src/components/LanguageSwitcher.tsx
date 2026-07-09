import { useLocale, useT, LOCALES } from '@hikrad/shared'

/** Segmented en/ar/ku switcher — required on the login page (task brief). */
export function LanguageSwitcher() {
  const { locale, setLocale } = useLocale()
  const t = useT()
  return (
    <div role="group" aria-label={t('portal.login.language')} className="inline-flex gap-1">
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
          {t(`common.languages.${code}`)}
        </button>
      ))}
    </div>
  )
}
