import { useEffect, useRef } from 'react'

import { useT } from '@hikrad/shared'

import { UserMenu } from './UserMenu'

function isEditable(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) return false
  return (
    target instanceof HTMLInputElement ||
    target instanceof HTMLTextAreaElement ||
    target.isContentEditable
  )
}

export function TopBar({ onOpenMenu }: { onOpenMenu: () => void }) {
  const t = useT()
  const searchRef = useRef<HTMLInputElement>(null)

  // FR-2 keyboard-first: '/' focuses the global search from anywhere.
  useEffect(() => {
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key === '/' && !e.ctrlKey && !e.metaKey && !e.altKey && !isEditable(e.target)) {
        e.preventDefault()
        searchRef.current?.focus()
      }
    }
    window.addEventListener('keydown', onKeyDown)
    return () => window.removeEventListener('keydown', onKeyDown)
  }, [])

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
      {/* Global-search placeholder slot — real search wires in here in Phase 2 (FR-2). */}
      <input
        ref={searchRef}
        type="search"
        aria-label={t('search.label')}
        placeholder={t('search.placeholder')}
        className="min-w-0 max-w-md flex-1 rounded-md border border-surface-sunken bg-surface px-3 py-1.5 text-sm placeholder:text-ink-muted focus:border-brand focus:outline-none"
      />
      <div className="ms-auto">
        <UserMenu />
      </div>
    </header>
  )
}
