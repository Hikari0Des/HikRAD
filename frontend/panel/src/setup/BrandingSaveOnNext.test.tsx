import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { I18nProvider } from '@hikrad/shared'
import en from '@hikrad/shared/locales/en/panel.json'

import { AuthProvider } from '../auth/AuthContext'
import { ToastProvider } from '../components/Toast'
import { SetupWizardPage } from './SetupWizardPage'

// The bug (FR-106.2): the branding step's own Next never triggered the embedded
// BrandingSettings' Save, so branding.name was never persisted. This mocks the
// settings-group save and asserts advancing from the branding step calls it.
const saveSpy = vi.fn().mockResolvedValue(undefined)
vi.mock('../pages/settings/useSettingsGroup', () => ({
  useSettingsGroup: () => ({
    loaded: true,
    values: { name: 'Acme ISP', primary_color: '#08748f', secondary_color: '#0f172a' },
    errors: {},
    saving: false,
    save: saveSpy,
    setField: vi.fn(),
  }),
}))

vi.mock('../api/setup', () => ({
  getSetupLicense: vi
    .fn()
    .mockResolvedValue({ installed: false, state: 'valid', fingerprint: 'x' }),
  uploadSetupLicense: vi.fn(),
  createSetupAdmin: vi.fn(),
}))

function renderWizard() {
  return render(
    <I18nProvider>
      <AuthProvider>
        <ToastProvider>
          <MemoryRouter>
            <SetupWizardPage onSetupComplete={vi.fn()} />
          </MemoryRouter>
        </ToastProvider>
      </AuthProvider>
    </I18nProvider>,
  )
}

beforeEach(() => {
  window.localStorage.clear()
  saveSpy.mockClear()
})

describe('SetupWizardPage branding step (FR-106.2): name persists on Next', () => {
  it('invokes the branding save when the wizard Next is pressed', async () => {
    window.localStorage.setItem('hikrad:setup:step', 'branding')
    const user = userEvent.setup()
    renderWizard()

    await user.click(await screen.findByRole('button', { name: en.ui.next }))
    expect(saveSpy).toHaveBeenCalledTimes(1)
    // And the wizard advanced to the NAS step.
    expect(window.localStorage.getItem('hikrad:setup:step')).toBe('nas')
  })

  it('offers a Back control on the NAS step and returns to branding', async () => {
    window.localStorage.setItem('hikrad:setup:step', 'profile')
    const user = userEvent.setup()
    renderWizard()
    // FR-106.1: profile → nas via Back.
    await user.click(await screen.findByRole('button', { name: `← ${en.ui.back}` }))
    expect(window.localStorage.getItem('hikrad:setup:step')).toBe('nas')
  })
})
