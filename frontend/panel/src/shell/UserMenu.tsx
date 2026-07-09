import * as DropdownMenu from '@radix-ui/react-dropdown-menu'

import { Ltr, useLocale, useT, type Locale } from '@hikrad/shared'

import { useAuth } from '../auth/AuthContext'

const LOCALES: readonly Locale[] = ['en', 'ar', 'ku']

export function UserMenu() {
  const { manager, logout } = useAuth()
  const { locale, dir, setLocale } = useLocale()
  const t = useT()

  if (!manager) return null

  return (
    <DropdownMenu.Root dir={dir}>
      <DropdownMenu.Trigger asChild>
        <button
          type="button"
          aria-label={t('user.menu')}
          className="flex items-center gap-2 rounded-full bg-surface-sunken px-3 py-1.5 text-sm hover:bg-brand-soft"
        >
          <span
            aria-hidden="true"
            className="flex h-6 w-6 items-center justify-center rounded-full bg-brand text-xs font-bold text-ink-inverse"
          >
            <Ltr>{manager.username.slice(0, 1).toUpperCase()}</Ltr>
          </span>
          <Ltr className="hidden sm:inline">{manager.username}</Ltr>
        </button>
      </DropdownMenu.Trigger>
      <DropdownMenu.Portal>
        <DropdownMenu.Content
          align="end"
          sideOffset={6}
          className="z-30 min-w-48 rounded-md border border-surface-sunken bg-surface-raised p-1 shadow-lg"
        >
          <DropdownMenu.Label className="px-3 py-2 text-xs text-ink-muted">
            {t('user.signedInAs')} <Ltr className="font-medium text-ink">{manager.username}</Ltr>
          </DropdownMenu.Label>
          <DropdownMenu.Separator className="my-1 h-px bg-surface-sunken" />
          <DropdownMenu.Label className="px-3 pt-1 text-xs text-ink-muted">
            {t('user.language')}
          </DropdownMenu.Label>
          {LOCALES.map((code) => (
            <DropdownMenu.Item
              key={code}
              onSelect={() => setLocale(code)}
              className="cursor-pointer rounded px-3 py-2 text-sm outline-none data-[highlighted]:bg-brand-soft"
            >
              <span className="flex items-center justify-between gap-4">
                {t(`languages.${code}`)}
                {locale === code && <span aria-hidden="true">✓</span>}
              </span>
            </DropdownMenu.Item>
          ))}
          <DropdownMenu.Separator className="my-1 h-px bg-surface-sunken" />
          <DropdownMenu.Item
            onSelect={logout}
            className="cursor-pointer rounded px-3 py-2 text-sm text-danger outline-none data-[highlighted]:bg-brand-soft"
          >
            {t('user.logout')}
          </DropdownMenu.Item>
        </DropdownMenu.Content>
      </DropdownMenu.Portal>
    </DropdownMenu.Root>
  )
}
