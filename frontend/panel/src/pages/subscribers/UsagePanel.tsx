import { useState } from 'react'

import { ErrorState, LoadingState, useT } from '@hikrad/shared'

import { usageBySubscriber } from '../../api/live'
import { Button } from '../../components/Button'
import { UsageChart } from '../../components/UsageChart'
import { useAsync } from '../../hooks/useAsync'

/** Daily/monthly usage graphs (FR-33, C7-C). Charts render LTR inside RTL. */
export function UsagePanel({ subscriberId }: { subscriberId: string }) {
  const t = useT()
  const [granularity, setGranularity] = useState<'daily' | 'monthly'>('daily')
  const { data, error, loading, reload } = useAsync(
    () => usageBySubscriber(subscriberId, granularity),
    [subscriberId, granularity],
  )

  return (
    <div>
      <div className="mb-3 inline-flex overflow-hidden rounded-md border border-surface-sunken text-sm">
        <button
          type="button"
          onClick={() => setGranularity('daily')}
          className={`px-3 py-1.5 ${granularity === 'daily' ? 'bg-brand text-ink-inverse' : ''}`}
        >
          {t('usage.daily')}
        </button>
        <button
          type="button"
          onClick={() => setGranularity('monthly')}
          className={`px-3 py-1.5 ${granularity === 'monthly' ? 'bg-brand text-ink-inverse' : ''}`}
        >
          {t('usage.monthly')}
        </button>
      </div>
      {error ? (
        <ErrorState onRetry={reload} />
      ) : loading ? (
        <LoadingState />
      ) : (
        <UsageChart points={data ?? []} granularity={granularity} />
      )}
      <div className="mt-2 text-end">
        <Button size="sm" variant="ghost" onClick={reload}>
          {t('ui.refresh')}
        </Button>
      </div>
    </div>
  )
}
