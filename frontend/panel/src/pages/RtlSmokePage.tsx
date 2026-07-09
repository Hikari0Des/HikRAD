import * as DropdownMenu from '@radix-ui/react-dropdown-menu'

import { Ltr, formatDate, formatIQD, useLocale, useT } from '@hikrad/shared'

import { LanguageSwitcher } from '../components/LanguageSwitcher'

/**
 * Bidirectional smoke page — proves the frozen component-library choice
 * (Tailwind + Radix + CSS logical properties, master PRD §8): every element
 * below must mirror when the locale flips to ar/ku, while usernames, IPs and
 * MACs stay LTR via the shared bidi-isolate component.
 */
export function RtlSmokePage() {
  const t = useT()
  const { locale, dir } = useLocale()

  return (
    <section className="space-y-6">
      <div>
        <h1 className="text-xl font-semibold">{t('smoke.title')}</h1>
        <p className="mt-1 text-sm text-ink-muted">{t('smoke.intro')}</p>
      </div>

      <LanguageSwitcher />

      <div className="flex flex-col gap-2">
        <div className="self-start rounded-md border-s-4 border-brand bg-surface-raised px-4 py-2 text-sm shadow-sm">
          {t('smoke.startAligned')}
        </div>
        <div className="self-end rounded-md border-e-4 border-brand bg-surface-raised px-4 py-2 text-sm shadow-sm">
          {t('smoke.endAligned')}
        </div>
      </div>

      <div className="rounded-lg bg-surface-raised p-4 shadow-sm">
        <p className="text-sm">
          {t('smoke.mixed', { username: 'ali99', gb: '4.2', ip: '10.5.1.77' })}
        </p>
        <p className="mt-2 text-sm text-ink-muted">{t('smoke.note')}</p>
        <p className="mt-2 flex flex-wrap gap-3 text-sm" data-testid="bidi-isolated">
          <Ltr className="font-mono">ali99</Ltr>
          <Ltr className="font-mono">AA:BB:CC:11:22:33</Ltr>
          <Ltr className="font-mono">10.5.1.77</Ltr>
          <span>{formatIQD(25000, locale)}</span>
          <span>{formatDate(new Date('2026-01-15T12:00:00Z'), locale)}</span>
        </p>
      </div>

      <div className="rounded-lg bg-surface-raised p-4 shadow-sm">
        <p className="mb-2 text-sm text-ink-muted">{t('smoke.radixLabel')}</p>
        <DropdownMenu.Root dir={dir}>
          <DropdownMenu.Trigger asChild>
            <button
              type="button"
              className="rounded-md bg-brand px-4 py-2 text-sm text-ink-inverse hover:bg-brand-strong"
            >
              {t('smoke.open')}
            </button>
          </DropdownMenu.Trigger>
          <DropdownMenu.Portal>
            <DropdownMenu.Content
              align="start"
              sideOffset={6}
              className="z-30 min-w-40 rounded-md border border-surface-sunken bg-surface-raised p-1 shadow-lg"
            >
              <DropdownMenu.Item className="cursor-pointer rounded px-3 py-2 text-sm outline-none data-[highlighted]:bg-brand-soft">
                {t('smoke.itemOne')}
              </DropdownMenu.Item>
              <DropdownMenu.Item className="cursor-pointer rounded px-3 py-2 text-sm outline-none data-[highlighted]:bg-brand-soft">
                {t('smoke.itemTwo')}
              </DropdownMenu.Item>
            </DropdownMenu.Content>
          </DropdownMenu.Portal>
        </DropdownMenu.Root>
      </div>
    </section>
  )
}
