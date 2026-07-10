import { useCallback, useEffect, useRef, useState } from 'react'

export interface AsyncState<T> {
  data: T | undefined
  error: unknown
  loading: boolean
  /** Re-run the loader (e.g. after a mutation or a Retry click). */
  reload: () => void
}

/**
 * Load-on-mount async data with loading/error state and a manual reload. Stale
 * responses (a reload issued before the previous resolved, or an unmount) are
 * dropped via a monotonically increasing call id, so the last request wins.
 * `deps` re-runs the loader like an effect dependency list.
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
