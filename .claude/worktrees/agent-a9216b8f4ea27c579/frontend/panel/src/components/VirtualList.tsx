import { useCallback, useRef, useState, type ReactNode } from 'react'

/**
 * Minimal fixed-row-height windowing (no dependency): only the visible slice
 * (plus an overscan margin) is rendered, so the Live Sessions table stays
 * smooth at 2k+ rows. The scroll container has a fixed height; a spacer of the
 * full content height preserves the scrollbar, and the visible rows are offset
 * with a transform.
 */
export function VirtualList<T>({
  items,
  rowHeight,
  height,
  renderRow,
  overscan = 8,
  getKey,
  className,
}: {
  items: T[]
  rowHeight: number
  height: number
  renderRow: (item: T, index: number) => ReactNode
  overscan?: number
  getKey: (item: T, index: number) => string
  className?: string
}) {
  const [scrollTop, setScrollTop] = useState(0)
  const ref = useRef<HTMLDivElement>(null)

  const onScroll = useCallback(() => {
    if (ref.current) setScrollTop(ref.current.scrollTop)
  }, [])

  const total = items.length
  const first = Math.max(0, Math.floor(scrollTop / rowHeight) - overscan)
  const visibleCount = Math.ceil(height / rowHeight) + overscan * 2
  const last = Math.min(total, first + visibleCount)
  const slice = items.slice(first, last)

  return (
    <div
      ref={ref}
      onScroll={onScroll}
      className={`overflow-auto ${className ?? ''}`}
      style={{ height }}
    >
      <div style={{ height: total * rowHeight, position: 'relative' }}>
        <div style={{ transform: `translateY(${first * rowHeight}px)` }}>
          {slice.map((item, i) => (
            <div key={getKey(item, first + i)} style={{ height: rowHeight }}>
              {renderRow(item, first + i)}
            </div>
          ))}
        </div>
      </div>
    </div>
  )
}
