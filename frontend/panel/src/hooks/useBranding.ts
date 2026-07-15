import { useEffect, useState } from 'react'

export interface Branding {
  name: string
  logo_url: string | null
}

/** Public branding read (contract C5), used for the report print header. */
export function useBranding(): Branding {
  const [branding, setBranding] = useState<Branding>({ name: 'HikRAD', logo_url: null })
  useEffect(() => {
    let cancelled = false
    fetch('/api/v1/branding', { headers: { Accept: 'application/json' } })
      .then((res) => (res.ok ? (res.json() as Promise<Branding>) : null))
      .then((b) => {
        if (!cancelled && b) setBranding(b)
      })
      .catch(() => {
        /* offline-first: keep the default name */
      })
    return () => {
      cancelled = true
    }
  }, [])
  return branding
}
