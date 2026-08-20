// Per-widget line icons for the dashboard cards (v3-1 SAMM restyle), same
// hand-drawn approach as the sidebar's navIcons: 24×24, stroke=currentColor,
// no fill. Rendered muted in the top-right tile of each card header so the KPI
// row reads like SAMM's rather than a bare label + number.
import type { ReactNode } from 'react'

const PATHS: Record<string, ReactNode> = {
  'online-now': <path d="M22 12h-4l-3 9L9 3l-3 9H2" />,
  'revenue-today': (
    <>
      <circle cx="12" cy="12" r="9" />
      <path d="M12 7v10M14.5 9.2A2.6 2.6 0 0 0 12 7.6c-1.5 0-2.6.9-2.6 2s1 1.7 2.6 2 2.6.9 2.6 2-1.1 2-2.6 2a2.6 2.6 0 0 1-2.5-1.6" />
    </>
  ),
  'radius-rps': <path d="M13 2 3 14h9l-1 8 10-12h-9l1-8Z" />,
  'subs-active': (
    <>
      <path d="M16 21v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2" />
      <circle cx="9" cy="7" r="4" />
      <path d="m16 11 2 2 4-4" />
    </>
  ),
  'subs-expired': (
    <>
      <circle cx="12" cy="12" r="9" />
      <path d="M12 7v5l3 2" />
    </>
  ),
  'subs-expiring': (
    <>
      <circle cx="12" cy="12" r="9" />
      <path d="M12 7v5l3 2" />
    </>
  ),
  'pipeline-health': (
    <>
      <circle cx="6" cy="6" r="2.5" />
      <circle cx="6" cy="18" r="2.5" />
      <circle cx="18" cy="12" r="2.5" />
      <path d="M8.5 6H14a2 2 0 0 1 2 2v2M8.5 18H14a2 2 0 0 0 2-2v-2" />
    </>
  ),
  'nas-health': (
    <>
      <rect x="2" y="3" width="20" height="7" rx="1.5" />
      <rect x="2" y="14" width="20" height="7" rx="1.5" />
      <path d="M6 6.5h.01M6 17.5h.01" />
    </>
  ),
  'my-balance': (
    <>
      <path d="M19 7V5a2 2 0 0 0-2-2H5a2 2 0 0 0 0 4h14a2 2 0 0 1 2 2v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V5" />
      <path d="M16 12h.01" />
    </>
  ),
  'pending-payment-tickets': (
    <>
      <path d="M22 12h-6l-2 3h-4l-2-3H2" />
      <path d="M5.5 5.1 2 12v6a2 2 0 0 0 2 2h16a2 2 0 0 0 2-2v-6l-3.5-6.9A2 2 0 0 0 16.7 4H7.3a2 2 0 0 0-1.8 1.1Z" />
    </>
  ),
  'alerts-feed': (
    <>
      <path d="M18 8a6 6 0 0 0-12 0c0 7-3 9-3 9h18s-3-2-3-9" />
      <path d="M13.7 21a2 2 0 0 1-3.4 0" />
    </>
  ),
  'top-usage-subscribers': (
    <>
      <path d="M3 3v18h18" />
      <path d="m19 9-5 5-4-4-4 4" />
    </>
  ),
  'top-session-subscribers': (
    <>
      <circle cx="12" cy="12" r="9" />
      <path d="M12 7v5l3 2" />
    </>
  ),
  // v4-12 (FR-174)
  'open-tickets': (
    <>
      <path d="M3 8a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2v2a2 2 0 0 0 0 4v2a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-2a2 2 0 0 0 0-4Z" />
      <path d="M13 7v10" strokeDasharray="2 2" />
    </>
  ),
  'routers-reachable': (
    <>
      <path d="M5 12.5a10 10 0 0 1 14 0" />
      <path d="M8.5 16a5 5 0 0 1 7 0" />
      <circle cx="12" cy="19" r="1.2" fill="currentColor" stroke="none" />
    </>
  ),
  'unpaid-invoices': (
    <>
      <path d="M6 2h9l3 3v17l-3-2-3 2-3-2-3 2V2Z" />
      <path d="M9 8h6M9 12h6M9 16h3" />
    </>
  ),
  'ops-health-panel': (
    <>
      <path d="M3 12h4l2 8 4-16 2 8h6" />
    </>
  ),
  'recent-coa-outbox': (
    <>
      <path d="M4 4h16v12H8l-4 4Z" />
      <path d="M8 9h8M8 12h5" />
    </>
  ),
  'recent-admin-actions': (
    <>
      <path d="M9 3h6l1 2h3v2H5V5h3Z" />
      <path d="M6 7v13a1 1 0 0 0 1 1h10a1 1 0 0 0 1-1V7" />
      <path d="M10 11v6M14 11v6" />
    </>
  ),
  'recent-subscribers': (
    <>
      <circle cx="9" cy="8" r="3" />
      <path d="M3 20v-1a5 5 0 0 1 5-5h2a5 5 0 0 1 5 5v1" />
      <path d="M16 5.5c1.4.3 2.5 1.6 2.5 3s-1.1 2.7-2.5 3" />
    </>
  ),
  'recent-router-health': (
    <>
      <rect x="3" y="9" width="18" height="6" rx="1.5" />
      <path d="M7 12h.01M11 12h.01" />
      <path d="M16 5v4M18 3v6" />
    </>
  ),
}

export function WidgetIcon({ name }: { name: string }) {
  const path = PATHS[name]
  if (!path) return null
  return (
    <span className="flex h-7 w-7 items-center justify-center rounded-lg bg-surface-sunken text-ink-muted">
      <svg
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        strokeWidth={1.75}
        strokeLinecap="round"
        strokeLinejoin="round"
        className="h-4 w-4"
        aria-hidden="true"
      >
        {path}
      </svg>
    </span>
  )
}
