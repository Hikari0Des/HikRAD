import { useT } from '@hikrad/shared'

/**
 * Fixed HikRAD attribution (v2 phase 11, FR-93, contract C10). Deliberately
 * the ONE branding surface that reads NOTHING from settings — no
 * useBranding, no fetch, no props. A customer may rename/re-color every
 * other surface (FR-91/92); this line never changes. Duplicated verbatim in
 * frontend/portal/src/shell/PoweredByFooter.tsx rather than shared, so a
 * rebrand can't be achieved by patching one file for both apps at once.
 */
export function PoweredByFooter() {
  const t = useT()
  return (
    <p className="select-none py-2 text-center text-[11px] text-ink-muted/70">
      {t('common.poweredBy')}
    </p>
  )
}
