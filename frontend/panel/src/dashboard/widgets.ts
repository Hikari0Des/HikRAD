/**
 * Dashboard widget catalog (v2-10, FR-89.1/89.2, contract C1). Mirrors
 * backend/internal/monitorsvc/dashboard_widgets.go exactly — same ids, same
 * order, same permission map. Never invent an id or permission here the
 * backend catalog doesn't also have: this list only drives what the picker
 * OFFERS, the server is the actual authority (C4) — a hand-crafted request
 * naming a forbidden id gets nothing back regardless of what this file says.
 */
import {
  PERM_LIVE_VIEW,
  PERM_MONITORING_VIEW,
  PERM_NAS_VIEW,
  PERM_PAYMENT_TICKETS_VERIFY,
  PERM_REPORTS_VIEW,
  PERM_SESSIONS_VIEW,
  PERM_SUBSCRIBERS_VIEW,
} from '../auth/permissions'

export type DashboardWidgetSize = '1x' | '2x'

export interface DashboardWidgetDef {
  id: string
  /** '' = every authenticated manager, no permission needed. */
  permission: string
  defaultSize: DashboardWidgetSize
}

export const DASHBOARD_WIDGET_CATALOG: readonly DashboardWidgetDef[] = [
  { id: 'online-now', permission: PERM_LIVE_VIEW, defaultSize: '1x' },
  { id: 'revenue-today', permission: PERM_REPORTS_VIEW, defaultSize: '1x' },
  { id: 'radius-rps', permission: PERM_MONITORING_VIEW, defaultSize: '1x' },
  { id: 'subs-active', permission: PERM_SUBSCRIBERS_VIEW, defaultSize: '1x' },
  { id: 'subs-expired', permission: PERM_SUBSCRIBERS_VIEW, defaultSize: '1x' },
  { id: 'subs-expiring', permission: PERM_SUBSCRIBERS_VIEW, defaultSize: '1x' },
  { id: 'pipeline-health', permission: PERM_MONITORING_VIEW, defaultSize: '2x' },
  { id: 'nas-health', permission: PERM_NAS_VIEW, defaultSize: '2x' },
  { id: 'my-balance', permission: '', defaultSize: '1x' },
  { id: 'pending-payment-tickets', permission: PERM_PAYMENT_TICKETS_VERIFY, defaultSize: '1x' },
  { id: 'alerts-feed', permission: PERM_MONITORING_VIEW, defaultSize: '2x' },
  { id: 'top-usage-subscribers', permission: PERM_SUBSCRIBERS_VIEW, defaultSize: '2x' },
  { id: 'top-session-subscribers', permission: PERM_SESSIONS_VIEW, defaultSize: '2x' },
] as const

export type DashboardWidgetId = (typeof DASHBOARD_WIDGET_CATALOG)[number]['id']

const BY_ID = new Map(DASHBOARD_WIDGET_CATALOG.map((w) => [w.id, w]))

export function widgetDef(id: string): DashboardWidgetDef | undefined {
  return BY_ID.get(id)
}

/** Every catalog widget `can` permits, in catalog order — FR-90.1's default layout. */
export function defaultWidgetIds(can: (permission: string) => boolean): DashboardWidgetId[] {
  return DASHBOARD_WIDGET_CATALOG.filter((w) => w.permission === '' || can(w.permission)).map(
    (w) => w.id,
  )
}
