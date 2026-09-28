import { Link } from 'react-router-dom'

import { useT } from '@hikrad/shared'

export function NotFoundPage() {
  const t = useT()
  return (
    <section className="py-16 text-center">
      <h1 className="text-xl font-semibold">{t('notFound.title')}</h1>
      <p className="mt-2 text-sm text-ink-muted">{t('notFound.body')}</p>
      <Link
        to="/"
        className="mt-6 inline-block rounded-md bg-brand px-4 py-2 text-sm text-ink-inverse hover:bg-brand-strong"
      >
        {t('notFound.home')}
      </Link>
    </section>
  )
}
