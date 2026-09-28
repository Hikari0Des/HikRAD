import { useT } from '@hikrad/shared'

import type { DeviceOwnership } from '../api/types'
import { Pill } from './Pill'

/**
 * The router-ownership badge (v4-6, FR-140): "system" = HikRAD-owned and
 * editable (v4-7), "router" = an adopted, read-only reference, "mixed" = a
 * tab whose individual rows carry their own badge (the IP tab's DHCP
 * servers). Always computed server-side and passed in — never inferred here.
 */
export function OwnershipBadge({ ownership }: { ownership: DeviceOwnership }) {
  const t = useT()
  if (ownership === 'system') {
    return <Pill tone="brand">{t('nas.device.ownership.system')}</Pill>
  }
  if (ownership === 'mixed') {
    return <Pill tone="muted">{t('nas.device.ownership.mixed')}</Pill>
  }
  return <Pill tone="muted">{t('nas.device.ownership.router')}</Pill>
}
