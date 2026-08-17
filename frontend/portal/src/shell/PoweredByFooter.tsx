import { useT } from '@hikrad/shared'

import { useBranding } from '../branding'

/**
 * Fixed HikRAD attribution (v2 phase 11, FR-93, contract C10; entitlement-
 * gated as of FR-122/Decision 62). The ONE thing this reads is
 * `white_label` off the public branding payload — a boolean the backend
 * derives fresh from the SIGNED licence's entitlement set
 * (platform.LicenseHasEntitlement), never from a settings row. There is no
 * settings key, no PUT, and deliberately NO UI control anywhere that can
 * hide this mark — the only way it disappears is a licence that grants
 * white_label. Duplicated verbatim in
 * frontend/panel/src/shell/PoweredByFooter.tsx rather than shared, so a
 * rebrand can't be achieved by patching one file for both apps at once.
 */
export function PoweredByFooter() {
  const t = useT()
  const { white_label } = useBranding()
  if (white_label) return null
  return (
    <p className="select-none py-2 text-center text-[11px] text-ink-muted/70">
      {t('common.poweredBy')}
    </p>
  )
}
