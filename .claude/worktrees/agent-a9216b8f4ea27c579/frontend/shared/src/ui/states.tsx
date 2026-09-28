import { useT } from '../i18n/I18nProvider'

/** Localized loading indicator. */
export function LoadingState({ label, className }: { label?: string; className?: string }) {
  const t = useT()
  return (
    <div className={`hk-state${className ? ` ${className}` : ''}`} role="status">
      <span className="hk-spinner" aria-hidden="true" />
      <span className="hk-state__body">{label ?? t('common.loading')}</span>
    </div>
  )
}

/** Localized empty state with overridable title/body. */
export function EmptyState({
  title,
  body,
  className,
}: {
  title?: string
  body?: string
  className?: string
}) {
  const t = useT()
  return (
    <div className={`hk-state${className ? ` ${className}` : ''}`}>
      <p className="hk-state__title">{title ?? t('common.empty.title')}</p>
      <p className="hk-state__body">{body ?? t('common.empty.body')}</p>
    </div>
  )
}

/** Localized error state with an optional retry action. */
export function ErrorState({
  title,
  body,
  onRetry,
  className,
}: {
  title?: string
  body?: string
  onRetry?: () => void
  className?: string
}) {
  const t = useT()
  return (
    <div className={`hk-state${className ? ` ${className}` : ''}`} role="alert">
      <p className="hk-state__title">{title ?? t('common.error.title')}</p>
      <p className="hk-state__body">{body ?? t('common.error.body')}</p>
      {onRetry && (
        <button type="button" className="hk-state__retry" onClick={onRetry}>
          {t('common.retry')}
        </button>
      )}
    </div>
  )
}
