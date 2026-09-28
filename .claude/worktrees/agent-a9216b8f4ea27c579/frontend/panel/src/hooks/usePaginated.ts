import { useCallback, useEffect, useRef, useState } from 'react'

import type { Page } from '../api/client'

export interface Paginated<T> {
  items: T[]
  loading: boolean
  error: unknown
  hasMore: boolean
  loadMore: () => void
  /** Discard accumulated items and reload the first page (filter change). */
  reset: () => void
}

/**
 * Accumulating cursor pagination over a C2 list endpoint. `fetchPage(cursor)`
 * returns one `{items,next_cursor}` page; changing `key` (e.g. a serialized
 * filter) resets and reloads from the first page. Stale pages are dropped by a
 * generation counter so a fast filter change can't interleave results.
 */
export function usePaginated<T>(
  fetchPage: (cursor?: string) => Promise<Page<T>>,
  key: string,
): Paginated<T> {
  const [items, setItems] = useState<T[]>([])
  const [cursor, setCursor] = useState<string | null>(null)
  const [hasMore, setHasMore] = useState(true)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<unknown>(undefined)
  const generation = useRef(0)
  const fetchRef = useRef(fetchPage)
  fetchRef.current = fetchPage

  const load = useCallback((gen: number, nextCursor: string | undefined) => {
    setLoading(true)
    setError(undefined)
    fetchRef
      .current(nextCursor)
      .then((page) => {
        if (gen !== generation.current) return
        setItems((prev) => (nextCursor ? [...prev, ...page.items] : page.items))
        setCursor(page.next_cursor)
        setHasMore(page.next_cursor !== null)
        setLoading(false)
      })
      .catch((err) => {
        if (gen !== generation.current) return
        setError(err)
        setLoading(false)
      })
  }, [])

  // Reset + first page whenever the key changes.
  useEffect(() => {
    const gen = ++generation.current
    setItems([])
    setCursor(null)
    setHasMore(true)
    load(gen, undefined)
  }, [key, load])

  const loadMore = useCallback(() => {
    if (loading || !hasMore || cursor === null) return
    load(generation.current, cursor)
  }, [loading, hasMore, cursor, load])

  const reset = useCallback(() => {
    const gen = ++generation.current
    setItems([])
    setCursor(null)
    setHasMore(true)
    load(gen, undefined)
  }, [load])

  return { items, loading, error, hasMore, loadMore, reset }
}
