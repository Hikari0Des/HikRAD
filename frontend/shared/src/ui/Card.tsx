import { type ReactNode } from 'react'

/**
 * v3-1 FR-97.2 surface primitives. Presentational, token-only (.hk-* classes in
 * ui.css) so they render in any consumer regardless of its Tailwind globs.
 */

/** A raised, bordered, tokenized surface. `pad` adds the standard inset. */
export function Card({
  children,
  pad = true,
  className,
}: {
  children: ReactNode
  pad?: boolean
  className?: string
}) {
  return (
    <div className={`hk-card${pad ? ' hk-card--pad' : ''}${className ? ` ${className}` : ''}`}>
      {children}
    </div>
  )
}

/** A single headline metric: label + value + optional hint/delta. */
export function StatCard({
  label,
  value,
  hint,
  delta,
  className,
}: {
  label: ReactNode
  value: ReactNode
  hint?: ReactNode
  /** Optional signed change indicator; `dir` colors it. */
  delta?: { text: ReactNode; dir: 'up' | 'down' }
  className?: string
}) {
  return (
    <Card className={className}>
      <div className="hk-stat">
        <span className="hk-stat__label">{label}</span>
        <span className="hk-stat__value">{value}</span>
        {delta ? <span className={`hk-stat__delta--${delta.dir}`}>{delta.text}</span> : null}
        {hint ? <span className="hk-stat__hint">{hint}</span> : null}
      </div>
    </Card>
  )
}

/** A titled content section with an optional header action slot. */
export function SectionCard({
  title,
  actions,
  children,
  className,
}: {
  title: ReactNode
  actions?: ReactNode
  children: ReactNode
  className?: string
}) {
  return (
    <section className={`hk-section${className ? ` ${className}` : ''}`}>
      <div className="hk-section__head">
        <h2 className="hk-section__title">{title}</h2>
        {actions ? <div>{actions}</div> : null}
      </div>
      <div className="hk-section__body">{children}</div>
    </section>
  )
}
