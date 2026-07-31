import type { ReactNode } from 'react'

/**
 * v3-1 SAMM restyle: a small rounded value chip for table cells (speed, quota
 * mode, tags) — the blue "10 Mbps ↑" pills on SAMM's Plans screen. Distinct
 * from `StatusBadge`, which is specifically the subscriber lifecycle badge with
 * a status dot; `Pill` is a neutral value chip with tone variants.
 */
const TONES = {
  brand: 'bg-brand-soft text-brand-strong',
  muted: 'bg-surface-sunken text-ink-muted',
  ok: 'bg-ok/15 text-ok',
  warn: 'bg-warn/15 text-warn',
  danger: 'bg-danger/15 text-danger',
} as const

export function Pill({
  children,
  tone = 'brand',
  className,
}: {
  children: ReactNode
  tone?: keyof typeof TONES
  className?: string
}) {
  return (
    <span
      className={`inline-flex items-center gap-1 whitespace-nowrap rounded-full px-2 py-0.5 text-xs font-medium ${TONES[tone]}${
        className ? ` ${className}` : ''
      }`}
    >
      {children}
    </span>
  )
}
