import type { TFunction } from '@hikrad/shared'

import { ApiError } from '../api/client'

/**
 * Localizes the error codes every router-touching endpoint shares
 * (autosetup/services/wizard/wan/firewall/update/hotspot-page). Extracted in
 * v4-7 because the same switch had already been copy-pasted into two modals
 * and this phase adds five more call sites.
 *
 * `preview_stale` is the interesting one: it is not a failure, it is the
 * hash gate doing its job — the router moved between preview and apply and
 * nothing was written. The message says so rather than reading as an error.
 */
export function routerErrorMessage(err: unknown, t: TFunction): string {
  if (err instanceof ApiError) {
    switch (err.code) {
      case 'router_unreachable':
        return t('nas.autoSetup.errUnreachable')
      case 'no_api_credentials':
        return t('nas.autoSetup.noCreds')
      case 'preview_stale':
        return t('nas.autoSetup.errStale')
      case 'not_adopted':
        return t('nas.serverMgmt.notAdopted')
      case 'ros_not_validated':
        return t('nas.autoSetup.rosNotValidated')
      case 'conflict':
        return t('nas.plan.abortedConflict')
      case 'nothing_to_push':
        return t('nas.hotspotPage.errNothingToPush')
      default:
        return err.message
    }
  }
  return err instanceof Error ? err.message : t('common.error.body')
}
