import { createContext, useContext, useEffect, useState, type ReactNode } from 'react'

import { getLicense, type LicenseResponse } from '../api/setup'

interface LicenseContextValue {
  license: LicenseResponse | null
  /** True once the license grace has fully expired — panel mutations are 403'd server-side. */
  isReadOnly: boolean
  reload: () => void
}

const LicenseContext = createContext<LicenseContextValue>({
  license: null,
  isReadOnly: false,
  reload: () => {},
})

const POLL_MS = 5 * 60_000

/**
 * License state (contract C4, FR-50): polled independently of any one page so
 * the grace/read-only banner and gating stay current across the whole panel.
 * Never blocks rendering — a failed poll just keeps the last known state.
 */
export function LicenseProvider({ children }: { children: ReactNode }) {
  const [license, setLicense] = useState<LicenseResponse | null>(null)

  function load() {
    getLicense()
      .then(setLicense)
      .catch(() => {
        /* best-effort: keep last known state */
      })
  }

  useEffect(() => {
    load()
    const id = setInterval(load, POLL_MS)
    return () => clearInterval(id)
  }, [])

  const isReadOnly = license?.state === 'expired_grace'

  return (
    <LicenseContext.Provider value={{ license, isReadOnly, reload: load }}>
      {children}
    </LicenseContext.Provider>
  )
}

export function useLicense(): LicenseContextValue {
  return useContext(LicenseContext)
}
