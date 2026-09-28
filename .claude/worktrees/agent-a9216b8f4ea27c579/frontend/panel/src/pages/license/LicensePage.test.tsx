import { render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { I18nProvider } from '@hikrad/shared'

import en from '@hikrad/shared/locales/en/panel.json'
import { App } from '../../App'
import { AuthProvider } from '../../auth/AuthContext'
import { BrandingProvider } from '../../branding'
import { ToastProvider } from '../../components/Toast'

// FR-125/gate item 4: the License page must name WHICH fingerprint
// component drifted (machine id, MAC, or both), not just that something
// changed. This exact behavior had no dedicated test before this phase.

function stubFetch(licenseBody: Record<string, unknown>) {
  vi.stubGlobal(
    'fetch',
    vi.fn((input: RequestInfo | URL) => {
      const url = typeof input === 'string' ? input : input.toString()
      if (url.includes('/api/v1/license') && !url.includes('request-blob')) {
        return Promise.resolve(new Response(JSON.stringify(licenseBody), { status: 200 }))
      }
      if (url.includes('/api/v1/branding')) {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              name: 'HikRAD',
              logo_url: null,
              theme_color: null,
              background_color: null,
              white_label: false,
            }),
            { status: 200 },
          ),
        )
      }
      return Promise.resolve(new Response(null, { status: 404 }))
    }),
  )
}

function renderLicensePage() {
  window.localStorage.setItem('hikrad.access_token', 'access-abc')
  window.localStorage.setItem('hikrad.refresh_token', 'refresh-def')
  window.localStorage.setItem(
    'hikrad.manager',
    JSON.stringify({ id: 'm1', username: 'admin', role: 'admin' }),
  )
  return render(
    <I18nProvider>
      <BrandingProvider>
        <ToastProvider>
          <MemoryRouter initialEntries={['/license']}>
            <AuthProvider>
              <App />
            </AuthProvider>
          </MemoryRouter>
        </ToastProvider>
      </BrandingProvider>
    </I18nProvider>,
  )
}

beforeEach(() => {
  window.localStorage.clear()
})

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('LicensePage drift diagnostic (FR-104/FR-125, gate item 4)', () => {
  it('names the machine id as the drifted component when only it changed', async () => {
    stubFetch({
      installed: true,
      state: 'grace',
      fingerprint: 'current-fp',
      issued_fingerprint: 'issued-fp',
      fingerprint_match: { machine_id_match: false, mac_match: true, within_tolerance: true },
    })
    renderLicensePage()
    await waitFor(() =>
      expect(screen.getByText(en.license.fingerprintDrift.machineId)).toBeInTheDocument(),
    )
    expect(screen.queryByText(en.license.fingerprintDrift.mac)).not.toBeInTheDocument()
    expect(screen.queryByText(en.license.fingerprintDrift.both)).not.toBeInTheDocument()
  })

  it('names the MAC as the drifted component when only it changed', async () => {
    stubFetch({
      installed: true,
      state: 'grace',
      fingerprint: 'current-fp',
      issued_fingerprint: 'issued-fp',
      fingerprint_match: { machine_id_match: true, mac_match: false, within_tolerance: true },
    })
    renderLicensePage()
    await waitFor(() =>
      expect(screen.getByText(en.license.fingerprintDrift.mac)).toBeInTheDocument(),
    )
  })

  it('names both components when both changed', async () => {
    stubFetch({
      installed: true,
      state: 'expired_grace',
      fingerprint: 'current-fp',
      issued_fingerprint: 'issued-fp',
      fingerprint_match: { machine_id_match: false, mac_match: false, within_tolerance: false },
    })
    renderLicensePage()
    await waitFor(() =>
      expect(screen.getByText(en.license.fingerprintDrift.both)).toBeInTheDocument(),
    )
  })

  it('shows no drift diagnostic when the fingerprint fully matches', async () => {
    stubFetch({
      installed: true,
      state: 'valid',
      fingerprint: 'same-fp',
      issued_fingerprint: 'same-fp',
      fingerprint_match: { machine_id_match: true, mac_match: true, within_tolerance: true },
    })
    renderLicensePage()
    await waitFor(() => expect(screen.getByText('same-fp')).toBeInTheDocument())
    expect(screen.queryByText(en.license.fingerprintDrift.machineId)).not.toBeInTheDocument()
    expect(screen.queryByText(en.license.fingerprintDrift.mac)).not.toBeInTheDocument()
    expect(screen.queryByText(en.license.fingerprintDrift.both)).not.toBeInTheDocument()
  })

  it('shows entitlement chips for a v2 license', async () => {
    stubFetch({
      installed: true,
      state: 'valid',
      fingerprint: 'fp',
      issued_fingerprint: 'fp',
      version: 2,
      entitlements: ['white_label', 'self_service'],
      fingerprint_match: { machine_id_match: true, mac_match: true, within_tolerance: true },
    })
    renderLicensePage()
    await waitFor(() =>
      expect(screen.getByText(en.license.entitlement.white_label)).toBeInTheDocument(),
    )
    expect(screen.getByText(en.license.entitlement.self_service)).toBeInTheDocument()
  })
})
