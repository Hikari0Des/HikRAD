import { useT } from '../i18n/I18nProvider'

/** Phase-1 subscriber statuses (contract C6); more arrive with later schema. */
export type SubscriberStatus = 'active' | 'expired' | 'disabled'

/** Localized colored badge for a subscriber status. */
export function StatusBadge({
  status,
  className,
}: {
  status: SubscriberStatus
  className?: string
}) {
  const t = useT()
  return (
    <span className={`hk-badge hk-badge--${status}${className ? ` ${className}` : ''}`}>
      <span className="hk-badge__dot" aria-hidden="true" />
      {t(`common.status.${status}`)}
    </span>
  )
}
