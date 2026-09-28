import type { ReactNode } from 'react'

import { ErrorState, LoadingState, useT } from '@hikrad/shared'

import { refreshDevice } from '../../../api/nasDevice'
import type { DeviceOwnership } from '../../../api/types'
import { Button } from '../../../components/Button'
import { OwnershipBadge } from '../../../components/OwnershipBadge'
import { StaleBadge } from '../../../components/StaleBadge'

/**
 * Shared chrome for every FR-140 device tab: ownership badge, staleness
 * marker, an explicit refresh action, and the two "nothing to show yet"
 * states (no saved API credentials vs. never sampled) — so a router that
 * can't be reached (or has never been granted credentials) renders a clear
 * explanation instead of an error page or a blank tab (gate item 2).
 */
export function DeviceTabShell({
  nasId,
  loading,
  error,
  reload,
  hasData,
  readAt,
  stale,
  ownership,
  noApiCredentials,
  children,
}: {
  nasId: string
  loading: boolean
  error: unknown
  reload: () => void
  hasData: boolean
  readAt: string | null
  stale: boolean
  ownership: DeviceOwnership
  noApiCredentials: boolean
  children: ReactNode
}) {
  const t = useT()

  if (error) return <ErrorState onRetry={reload} />
  if (loading) return <LoadingState />

  async function runRefresh() {
    await refreshDevice(nasId)
    setTimeout(reload, 3000)
  }

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="flex items-center gap-2">
          <OwnershipBadge ownership={ownership} />
          <StaleBadge stale={stale} readAt={readAt} />
        </div>
        <Button size="sm" variant="secondary" onClick={() => void runRefresh()}>
          {t('nas.device.refreshNow')}
        </Button>
      </div>
      {noApiCredentials ? (
        <p className="rounded-md border border-dashed border-border bg-surface-raised p-6 text-center text-sm text-ink-muted">
          {t('nas.device.noApiCredentials')}
        </p>
      ) : !hasData ? (
        <p className="rounded-md border border-dashed border-border bg-surface-raised p-6 text-center text-sm text-ink-muted">
          {t('nas.device.neverSampledBody')}
        </p>
      ) : (
        children
      )}
    </div>
  )
}
