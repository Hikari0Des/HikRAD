import { type ReactNode } from 'react'

import { EmptyState, ErrorState, LoadingState } from './states'

/**
 * v3-1 FR-97.2 data table. A thin, presentational, token-only table with a
 * sticky header, optional compact density, built-in loading/empty/error states,
 * and its own horizontal-scroll container (AC-95a — the body never scrolls the
 * page). Generic over the row type; columns declare how to render each cell.
 *
 * Deliberately not a data-grid: no sorting/paging/selection logic lives here —
 * those stay in the screen (server round-trips, per-manager persistence). This
 * only standardizes the shell so every list looks and behaves the same.
 */
export interface Column<T> {
  /** Stable key for React + the header cell. */
  key: string
  header: ReactNode
  cell: (row: T) => ReactNode
  /** Cell alignment; defaults to start. */
  align?: 'start' | 'end'
  /** Extra class on both the header and body cell. */
  className?: string
}

export function DataTable<T>({
  columns,
  rows,
  getRowKey,
  loading,
  error,
  onRetry,
  emptyTitle,
  emptyBody,
  density = 'normal',
  minInlineSize,
  onRowClick,
}: {
  columns: Column<T>[]
  rows: T[]
  getRowKey: (row: T) => string
  loading?: boolean
  error?: boolean
  onRetry?: () => void
  emptyTitle?: string
  emptyBody?: string
  density?: 'normal' | 'compact'
  /** Minimum table width before the wrapper scrolls (e.g. '48rem'). */
  minInlineSize?: string
  onRowClick?: (row: T) => void
}) {
  // States render in place of the table body region, never a broken shell.
  if (error) return <ErrorState onRetry={onRetry} />
  if (loading && rows.length === 0) return <LoadingState />
  if (!loading && rows.length === 0) return <EmptyState title={emptyTitle} body={emptyBody} />

  return (
    <div className="hk-table-wrap">
      <table
        className={`hk-table${density === 'compact' ? ' hk-table--compact' : ''}`}
        style={minInlineSize ? { minInlineSize } : undefined}
      >
        <thead>
          <tr>
            {columns.map((c) => (
              <th key={c.key} className={cellClass(c)}>
                {c.header}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {rows.map((row) => (
            <tr
              key={getRowKey(row)}
              onClick={onRowClick ? () => onRowClick(row) : undefined}
              style={onRowClick ? { cursor: 'pointer' } : undefined}
            >
              {columns.map((c) => (
                <td key={c.key} className={cellClass(c)}>
                  {c.cell(row)}
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

function cellClass<T>(c: Column<T>): string | undefined {
  const parts: string[] = []
  if (c.align === 'end') parts.push('hk-td--end')
  if (c.className) parts.push(c.className)
  return parts.length ? parts.join(' ') : undefined
}
