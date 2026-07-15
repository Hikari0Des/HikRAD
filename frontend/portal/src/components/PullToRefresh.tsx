import { useRef, useState, type ReactNode } from 'react'

import { useT } from '@hikrad/shared'

const TRIGGER_DISTANCE = 64

/**
 * Minimal touch-driven pull-to-refresh (task 1: "Skeleton loading, pull-to-
 * refresh"). Only engages when the scroll container is already at the top —
 * it never fights native page scrolling. No dependency: a handful of touch
 * listeners plus a translateY on the content.
 */
export function PullToRefresh({
  onRefresh,
  children,
}: {
  onRefresh: () => Promise<void>
  children: ReactNode
}) {
  const t = useT()
  const [pull, setPull] = useState(0)
  const [refreshing, setRefreshing] = useState(false)
  const startY = useRef<number | null>(null)
  const containerRef = useRef<HTMLDivElement>(null)

  function onTouchStart(e: React.TouchEvent) {
    if (refreshing) return
    if ((containerRef.current?.scrollTop ?? 0) > 0) {
      startY.current = null
      return
    }
    startY.current = e.touches[0].clientY
  }

  function onTouchMove(e: React.TouchEvent) {
    if (startY.current === null) return
    const delta = e.touches[0].clientY - startY.current
    if (delta > 0) setPull(Math.min(delta, TRIGGER_DISTANCE * 1.5))
  }

  async function onTouchEnd() {
    if (startY.current === null) return
    startY.current = null
    if (pull >= TRIGGER_DISTANCE) {
      setRefreshing(true)
      setPull(TRIGGER_DISTANCE)
      try {
        await onRefresh()
      } finally {
        setRefreshing(false)
        setPull(0)
      }
    } else {
      setPull(0)
    }
  }

  return (
    <div
      ref={containerRef}
      onTouchStart={onTouchStart}
      onTouchMove={onTouchMove}
      onTouchEnd={onTouchEnd}
      className="relative"
    >
      <div
        className="flex items-center justify-center overflow-hidden text-xs text-ink-muted transition-[height]"
        style={{ blockSize: pull }}
      >
        {pull > 0 ? (
          <span aria-live="polite">
            {refreshing ? t('portal.common.refreshing') : t('portal.common.pullToRefresh')}
          </span>
        ) : null}
      </div>
      {children}
    </div>
  )
}
