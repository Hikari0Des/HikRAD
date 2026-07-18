import { render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { I18nProvider } from '@hikrad/shared'

import en from '@hikrad/shared/locales/en/common.json'
import { App } from '../App'
import { AuthProvider } from '../auth/AuthContext'
import { BrandingProvider } from '../branding'

// Fixed HikRAD attribution (v2 phase 11, FR-93, gate item 14): present on
// both the authenticated shell and the login screen, even when a full
// custom identity is configured — the two coexist rather than one crowding
// out the other.

function stubFetch() {
  vi.stubGlobal(
    'fetch',
    vi.fn((input: RequestInfo | URL) => {
      const url = typeof input === 'string' ? input : input.toString()
      if (url.includes('/api/v1/branding')) {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              name: 'Nur Net',
              logo_url: null,
              theme_color: '#08748f',
              background_color: '#0f172a',
            }),
            { status: 200 },
          ),
        )
      }
      // Any other call (e.g. /portal/me on the home page) — a harmless 404
      // is enough; this test only cares about the shell chrome.
      return Promise.resolve(new Response(null, { status: 404 }))
    }),
  )
}

function renderAt(path: string) {
  return render(
    <I18nProvider>
      <BrandingProvider>
        <MemoryRouter initialEntries={[path]}>
          <AuthProvider>
            <App />
          </AuthProvider>
        </MemoryRouter>
      </BrandingProvider>
    </I18nProvider>,
  )
}

beforeEach(() => {
  window.localStorage.clear()
  stubFetch()
})

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('PoweredByFooter (FR-93)', () => {
  it('renders on the authenticated shell alongside a fully configured custom identity', async () => {
    window.localStorage.setItem('hikrad.portal.access_token', 'access-abc')
    window.localStorage.setItem('hikrad.portal.refresh_token', 'refresh-def')
    window.localStorage.setItem(
      'hikrad.portal.subscriber',
      JSON.stringify({ id: 's1', username: 'noor01' }),
    )

    renderAt('/home')

    expect(await screen.findByText('Nur Net')).toBeInTheDocument()
    await waitFor(() => expect(screen.getByText(en.common.poweredBy)).toBeInTheDocument())
  })

  it('renders on the login screen (pre-auth)', async () => {
    renderAt('/')

    expect(await screen.findByText('Nur Net')).toBeInTheDocument()
    expect(screen.getByText(en.common.poweredBy)).toBeInTheDocument()
  })
})
