import type { DashboardLayout } from '../api/preferences'
import {
  defaultWidgetIds,
  widgetDef,
  type DashboardWidgetId,
  type DashboardWidgetSize,
} from './widgets'

export interface EffectiveWidget {
  id: DashboardWidgetId
  size: DashboardWidgetSize
}

/**
 * Resolves a manager's effective dashboard layout (FR-90.1): the stored
 * layout if one exists, filtered to widgets `can` still permits (a
 * permission revoked after saving simply drops that entry at render time —
 * FR-90.3, never a broken tile); otherwise every permitted catalog widget at
 * its default size, in catalog order (the default layout).
 */
export function resolveLayout(
  stored: DashboardLayout | null | undefined,
  can: (permission: string) => boolean,
): EffectiveWidget[] {
  if (!stored) {
    return defaultWidgetIds(can).map((id) => ({ id, size: widgetDef(id)!.defaultSize }))
  }
  const out: EffectiveWidget[] = []
  for (const w of stored.widgets) {
    const def = widgetDef(w.id)
    if (!def) continue
    if (def.permission !== '' && !can(def.permission)) continue
    out.push({ id: w.id as DashboardWidgetId, size: w.size === '2x' ? '2x' : '1x' })
  }
  return out
}
