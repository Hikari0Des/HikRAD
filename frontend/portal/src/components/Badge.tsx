import type { ReactNode } from 'react'

/** Generic status pill, same `hk-badge` styling StatusBadge (@hikrad/shared)
 * uses for subscriber status — that component's status union is narrowly
 * typed to active/expired/disabled, so ticket/invoice statuses (a different,
 * larger set) get this small sibling instead of a type-unsafe cast. */
export function Badge({ tone, children }: { tone: string; children: ReactNode }) {
  return (
    <span className={`hk-badge hk-badge--${tone}`}>
      <span className="hk-badge__dot" aria-hidden="true" />
      {children}
    </span>
  )
}
