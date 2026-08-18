import { useFormatters, useT } from '@hikrad/shared'

import type { LimitProgress } from '../../api/types'
import { formatBytes } from '../../lib/units'

/**
 * FR-135 list-depth cell: compact quota/uptime/daily bars, days remaining,
 * and — when a switch_plan exhaustion action is currently in force — the
 * arrow-joined plan-swap badge. Renders nothing for a limit that isn't
 * configured (limit === 0), so an unlimited plan's row stays uncluttered.
 */
export function LimitProgressCell({ limits }: { limits: LimitProgress | undefined }) {
  const t = useT()
  const { formatNumber } = useFormatters()
  if (!limits) return <span className="text-ink-muted">—</span>

  const bytes = (n: number) => formatBytes(n, formatNumber)
  const quotaLimit =
    limits.quota_mode === 'total'
      ? limits.quota_total_bytes
      : limits.quota_down_bytes + limits.quota_up_bytes
  const quotaUsed =
    limits.quota_mode === 'total'
      ? limits.quota_used_bytes
      : limits.quota_used_down + limits.quota_used_up

  return (
    <div className="flex min-w-[10rem] flex-col gap-1">
      {limits.plan_swap.active && (
        <span
          className="inline-flex w-fit items-center gap-1 rounded bg-warn/10 px-1.5 py-0.5 text-xs text-warn"
          title={t('users.planSwapHint')}
        >
          {limits.plan_swap.to_profile_name ?? t('users.planSwapActive')}
        </span>
      )}
      {quotaLimit > 0 && (
        <Bar
          label={t('users.limitQuota')}
          used={quotaUsed}
          limit={quotaLimit}
          text={`${bytes(quotaUsed)} / ${bytes(quotaLimit)}`}
        />
      )}
      {limits.daily_limit_bytes > 0 && (
        <Bar
          label={t('users.limitDaily')}
          used={limits.daily_used_bytes}
          limit={limits.daily_limit_bytes}
          text={`${bytes(limits.daily_used_bytes)} / ${bytes(limits.daily_limit_bytes)}`}
        />
      )}
      {limits.uptime_limit_seconds > 0 && (
        <Bar
          label={t('users.limitUptime')}
          used={limits.uptime_used_seconds}
          limit={limits.uptime_limit_seconds}
          text={`${formatHours(limits.uptime_used_seconds)} / ${formatHours(limits.uptime_limit_seconds)}`}
        />
      )}
      {limits.days_remaining !== null && (
        <span
          className={`text-xs ${limits.days_remaining < 0 ? 'text-danger' : limits.days_remaining <= 3 ? 'text-warn' : 'text-ink-muted'}`}
        >
          {limits.days_remaining < 0
            ? t('subscriber.expiredAgo', { days: Math.abs(limits.days_remaining) })
            : t('users.daysRemaining', { days: formatNumber(limits.days_remaining) })}
        </span>
      )}
    </div>
  )
}

function Bar({
  label,
  used,
  limit,
  text,
}: {
  label: string
  used: number
  limit: number
  text: string
}) {
  const pct = limit > 0 ? Math.min(100, Math.round((used / limit) * 100)) : 0
  const color = pct >= 100 ? 'bg-danger' : pct >= 80 ? 'bg-warn' : 'bg-brand'
  return (
    <div>
      <div className="flex items-center justify-between text-[10px] text-ink-muted">
        <span>{label}</span>
        <span>{text}</span>
      </div>
      <div
        role="progressbar"
        aria-label={label}
        aria-valuenow={pct}
        aria-valuemin={0}
        aria-valuemax={100}
        className="h-1.5 w-full overflow-hidden rounded-full bg-surface-sunken"
      >
        <div className={`h-full rounded-full ${color}`} style={{ width: `${pct}%` }} />
      </div>
    </div>
  )
}

function formatHours(seconds: number): string {
  return `${Math.round(seconds / 3600)}h`
}
