import { useCallback, useEffect, useRef, useState } from 'react'

export interface AsyncState<T> {
  data: T | undefined
  error: unknown
  loading: boolean
  /** Re-run the loader (e.g. pull-to-refresh or a Retry click). */
  reload: () => void
}

/**
 * Load-on-mount async data with loading/error state and a manual reload
 * (mirrors frontend/panel/src/hooks/useAsync.ts). Stale responses are dropped
 * via a monotonically increasing call id, so the last request always wins —
 * important on Iraqi mobile networks where a retry can race the original.
 */
export function useAsync<T>(loader: () => Promise<T>, deps: unknown[]): AsyncState<T> {
  const [data, setData] = useState<T | undefined>(undefined)
  const [error, setError] = useState<unknown>(undefined)
  const [loading, setLoading] = useState(true)
  const callId = useRef(0)
  const mounted = useRef(true)

  useEffect(() => {
    mounted.current = true
    return () => {
      mounted.current = false
    }
  }, [])

  const run = useCallback(() => {
    const id = ++callId.current
    setLoading(true)
    setError(undefined)
    loader()
      .then((result) => {
        if (!mounted.current || id !== callId.current) return
        setData(result)
        setLoading(false)
      })
      .catch((err) => {
        if (!mounted.current || id !== callId.current) return
        setError(err)
        setLoading(false)
      })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, deps)

  useEffect(run, [run])

  return { data, error, loading, reload: run }
}
