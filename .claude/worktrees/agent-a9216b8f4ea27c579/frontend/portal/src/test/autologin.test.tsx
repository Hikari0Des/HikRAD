import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { I18nProvider } from '@hikrad/shared'

import { App } from '../App'
import { AuthProvider } from '../auth/AuthContext'
import { BrandingProvider } from '../branding'

import en from '../../../shared/locales/en/portal.json'

function jsonResponse(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

const fetchMock = vi.fn()

beforeEach(() => {
  fetchMock.mockReset()
  vi.stubGlobal('fetch', fetchMock)
  window.localStorage.clear()
})

afterEach(() => {
  vi.unstubAllGlobals()
})

function renderPortal(route = '/') {
  return render(
    <I18nProvider>
      <BrandingProvider>
        <MemoryRouter initialEntries={[route]}>
          <AuthProvider>
            <App />
          </AuthProvider>
        </MemoryRouter>
      </BrandingProvider>
    </I18nProvider>,
  )
}

describe('FR-167 auto-login (frontend behavior)', () => {
  it('signs the subscriber straight into Home when the auto-login probe succeeds', async () => {
    fetchMock.mockImplementation((url: string) => {
      if (String(url).includes('/portal/session/auto')) {
        return Promise.resolve(
          jsonResponse(200, {
            access_token: 'a1',
            refresh_token: 'r1',
            subscriber: { id: 's1', username: '+9647700000000', name: 'Noor', language: 'en' },
            scope: 'reduced',
          }),
        )
      }
      if (String(url).includes('/portal/me')) {
        return Promise.resolve(
          jsonResponse(200, {
            status: 'active',
            online_now: true,
            expires_at: null,
            days_left: 0,
            usage: { used_down: 0, used_up: 0, used_total: 0 },
            speed: { profile_down: 1024, profile_up: 512 },
            profile_name: 'Basic',
            throttle: {},
          }),
        )
      }
      if (String(url).includes('/portal/payment-tickets/latest')) {
        return Promise.resolve(jsonResponse(200, null))
      }
      return Promise.resolve(jsonResponse(404, { error: { code: 'not_found', message: 'no' } }))
    })

    renderPortal('/')

    // Never a flash of the login form when auto-login succeeds — the home
    // screen (or, at minimum, no login title) is what lands.
    expect(await screen.findByText(en.portal.home.title)).toBeInTheDocument()
    expect(screen.queryByText(en.portal.login.title)).not.toBeInTheDocument()
  })

  it('falls back to the ordinary login screen, silently, when auto-login refuses', async () => {
    fetchMock.mockResolvedValue(
      jsonResponse(401, { error: { code: 'auto_login_refused', message: 'no' } }),
    )

    renderPortal('/')

    expect(await screen.findByText(en.portal.login.title)).toBeInTheDocument()
    // A refusal is a normal outcome, never a user-facing error banner.
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })

  it('shows the reduced-scope banner and gates Settings behind a password step-up', async () => {
    fetchMock.mockImplementation((url: string) => {
      if (String(url).includes('/portal/session/auto')) {
        return Promise.resolve(
          jsonResponse(200, {
            access_token: 'a1',
            refresh_token: 'r1',
            subscriber: { id: 's1', username: '+9647700000000', name: 'Noor', language: 'en' },
            scope: 'reduced',
          }),
        )
      }
      if (String(url).includes('/portal/me')) {
        return Promise.resolve(
          jsonResponse(200, {
            status: 'active',
            online_now: true,
            expires_at: null,
            days_left: 0,
            usage: { used_down: 0, used_up: 0, used_total: 0 },
            speed: { profile_down: 1024, profile_up: 512 },
            profile_name: 'Basic',
            throttle: {},
          }),
        )
      }
      if (String(url).includes('/portal/payment-tickets/latest')) {
        return Promise.resolve(jsonResponse(200, null))
      }
      return Promise.resolve(jsonResponse(404, { error: { code: 'not_found', message: 'no' } }))
    })

    renderPortal('/settings')

    expect(await screen.findByText(en.portal.stepUp.settingsGateText)).toBeInTheDocument()
    // The full account-edit form (name field) must NOT be reachable at all
    // while reduced — this is UX only (the server enforces the real
    // restriction), but the UI must not even offer it.
    expect(screen.queryByLabelText(en.portal.settings.nameLabel)).not.toBeInTheDocument()
  })
})
