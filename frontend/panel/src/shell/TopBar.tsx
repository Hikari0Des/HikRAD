import { useT } from '@hikrad/shared'

import { GlobalSearch } from '../components/GlobalSearch'
import { LocaleToggle, ThemeToggle } from '../components/QuickToggles'
import { BalanceWidget } from './BalanceWidget'
import { NotificationBell } from './notifications'
import { UserMenu } from './UserMenu'

export function TopBar({ onOpenMenu }: { onOpenMenu: () => void }) {
  const t = useT()

  return (
    <header className="sticky top-0 z-10 flex items-center gap-3 border-b border-surface-sunken bg-surface-raised px-3 py-2 sm:px-4">
      <button
        type="button"
        onClick={onOpenMenu}
        aria-label={t('nav.menu')}
        className="rounded-md p-2 hover:bg-surface-sunken md:hidden"
      >
        <svg
          aria-hidden="true"
          width="20"
          height="20"
          viewBox="0 0 20 20"
          fill="none"
          stroke="currentColor"
          strokeWidth="2"
          strokeLinecap="round"
        >
          <path d="M3 5h14M3 10h14M3 15h14" />
        </svg>
      </button>
      {/* FR-2 global search: '/' shortcut lives inside the component. */}
      <GlobalSearch />
      <div className="ms-auto flex items-center gap-1 sm:gap-2">
        <ThemeToggle />
        <LocaleToggle />
        <BalanceWidget />
        <NotificationBell />
        <UserMenu />
      </div>
    </header>
  )
}
