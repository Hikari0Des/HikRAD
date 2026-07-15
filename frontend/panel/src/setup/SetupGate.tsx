import { type ReactNode } from 'react'

import { LoadingState } from '@hikrad/shared'

import { getSetupStatus } from '../api/setup'
import { useAsync } from '../hooks/useAsync'
import { SetupWizardPage } from './SetupWizardPage'

/**
 * First-run gate (FR-49.3): while no manager exists yet, the whole app is the
 * setup wizard — there is nothing else to route to (no session is possible).
 * Once an admin exists this renders the normal app unconditionally.
 */
export function SetupGate({ children }: { children: ReactNode }) {
  const { data, loading, reload } = useAsync(() => getSetupStatus(), [])

  if (loading) {
    return (
      <div className="flex min-h-screen items-center justify-center">
        <LoadingState />
      </div>
    )
  }
  if (data && !data.admin_exists) {
    return <SetupWizardPage onSetupComplete={reload} />
  }
  return <>{children}</>
}
