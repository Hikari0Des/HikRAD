/** Public branding read — contract C5 (`GET /api/v1/branding`, D). Used for
 * the login page, app shell header, and the branded PWA manifest override. */
import { request } from './client'

export interface Branding {
  name: string
  logo_url: string | null
  theme_color: string | null
  background_color: string | null
  /** FR-122/Decision 62: true only when a signed licence entitlement grants
   * it. There is no settings key or panel control that sets this — see
   * PoweredByFooter.tsx. */
  white_label: boolean
}

const FALLBACK: Branding = {
  name: 'HikRAD',
  logo_url: null,
  theme_color: null,
  background_color: null,
  white_label: false,
}

export async function getBranding(): Promise<Branding> {
  try {
    return await request<Branding>('/branding', { anonymous: true })
  } catch {
    // Offline-first (NFR-7): a missing/unreachable branding endpoint falls
    // back to the generic product identity rather than breaking the login
    // screen or the manifest.
    return FALLBACK
  }
}
