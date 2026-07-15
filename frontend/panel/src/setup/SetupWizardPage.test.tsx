import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { I18nProvider } from '@hikrad/shared'
import en from '@hikrad/shared/locales/en/panel.json'

import { AuthProvider } from '../auth/AuthContext'
import { ToastProvider } from '../components/Toast'
import { SetupWizardPage } from './SetupWizardPage'

vi.mock('../api/setup', () => ({
  getSetupLicense: vi
    .fn()
    .mockResolvedValue({ installed: false, state: 'valid', fingerprint: 'fp-1' }),
  uploadSetupLicense: vi.fn(),
  createSetupAdmin: vi.fn(),
}))

function renderWizard(onSetupComplete: () => void = vi.fn()) {
  return render(
    <I18nProvider>
      <AuthProvider>
        <ToastProvider>
          <MemoryRouter>
            <SetupWizardPage onSetupComplete={onSetupComplete} />
          </MemoryRouter>
        </ToastProvider>
      </AuthProvider>
    </I18nProvider>,
  )
}

beforeEach(() => {
  window.localStorage.clear()
})

describe('SetupWizardPage (task 4, FR-49.3): resumable stepper', () => {
  it('starts at the license step on a fresh run', async () => {
    renderWizard()
    expect(await screen.findByText(en.setup.license.body)).toBeInTheDocument()
  })

  it('resumes from the persisted step after a reload', async () => {
    window.localStorage.setItem('hikrad:setup:step', 'done')
    renderWizard()
    expect(await screen.findByText(en.setup.done.title)).toBeInTheDocument()
    // The license/admin steps are not re-shown.
    expect(screen.queryByText(en.setup.license.body)).not.toBeInTheDocument()
  })

  it('clears the persisted step and invalidates the setup gate once the wizard finishes', async () => {
    window.localStorage.setItem('hikrad:setup:step', 'done')
    const onSetupComplete = vi.fn()
    const user = userEvent.setup()
    renderWizard(onSetupComplete)
    await user.click(await screen.findByRole('button', { name: en.setup.done.goToDashboard }))
    expect(window.localStorage.getItem('hikrad:setup:step')).toBeNull()
    // SetupGate only checks admin_exists once on mount and sits above the
    // router, so a plain client-side navigate never got it to re-render the
    // real app after finishing — this must be called to actually leave the
    // wizard (found live in the Phase-5 M4 gate rehearsal: "Go to dashboard"
    // did nothing until a manual page refresh).
    expect(onSetupComplete).toHaveBeenCalledTimes(1)
  })
})
