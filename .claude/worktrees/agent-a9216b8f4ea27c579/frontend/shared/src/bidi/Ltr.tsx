import type { ReactNode } from 'react'

/**
 * Bidi isolation for machine-formatted values — usernames, MACs, IPs, phone
 * numbers, code/config snippets — so they keep left-to-right order inside RTL
 * sentences (NFR-6.2). Inline; use for values embedded in text.
 */
export function Ltr({ children, className }: { children: ReactNode; className?: string }) {
  return (
    <bdi dir="ltr" className={className}>
      {children}
    </bdi>
  )
}

/**
 * Chart-container convention (NFR-6.2): charts always render LTR, even inside
 * RTL pages — axes, time series and legends do not mirror. Wrap every chart
 * (and only charts) in this container; it pins direction without affecting
 * the surrounding logical-property layout.
 */
export function ChartContainer({
  children,
  className,
}: {
  children: ReactNode
  className?: string
}) {
  return (
    <div dir="ltr" className={className}>
      {children}
    </div>
  )
}
