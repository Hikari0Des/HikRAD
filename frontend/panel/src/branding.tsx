import { createContext, useContext, useEffect, useState, type ReactNode } from 'react'

import { getBranding, type Branding } from './api/branding'

/**
 * Instance identity (v2 phase 11, FR-91/FR-92): name/logo/colors read once
 * from the public branding endpoint (contract C5) and applied at runtime —
 * no rebuild per ISP. Falls back to the generic HikRAD identity while
 * loading or if the endpoint is unreachable (NFR-7), so the panel is never
 * blocked on it. Mirrors frontend/portal/src/branding.tsx exactly; the two
 * apps intentionally do not share this context (see the fixed-attribution
 * footer's own C10 rationale for why panel/portal duplication is
 * deliberate in this area of the codebase).
 */
const FALLBACK: Branding = {
  name: 'HikRAD',
  logo_url: null,
  theme_color: null,
  background_color: null,
}

const BrandingContext = createContext<Branding>(FALLBACK)

export function BrandingProvider({ children }: { children: ReactNode }) {
  const [branding, setBranding] = useState<Branding>(FALLBACK)

  useEffect(() => {
    let cancelled = false
    getBranding().then((b) => {
      if (!cancelled) setBranding(b)
    })
    return () => {
      cancelled = true
    }
  }, [])

  useEffect(() => {
    document.title = branding.name
  }, [branding.name])

  return <BrandingContext.Provider value={branding}>{children}</BrandingContext.Provider>
}

export function useBranding(): Branding {
  return useContext(BrandingContext)
}

/** Single-letter mark shown until a logo is uploaded/loaded. */
export function brandInitial(name: string): string {
  return name.trim().charAt(0).toUpperCase() || 'H'
}
