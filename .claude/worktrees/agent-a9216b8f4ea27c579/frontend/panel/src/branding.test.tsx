import { render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { I18nProvider } from '@hikrad/shared'

import { App } from './App'
import { AuthProvider } from './auth/AuthContext'
import { BrandingProvider } from './branding'

// Panel threading (v2 phase 11, FR-92, gate item 9): the sidebar, browser
// title, and login screen consume the configured instance identity instead
// of the hardcoded "HikRAD" product name they showed before this phase.

const CONFIGURED = {
  name: 'Nur Net',
  logo_url: null,
  theme_color: '#08748f',
  background_color: '#0f172a',
}

function stubBrandingFetch() {
  vi.stubGlobal(
    'fetch',
    vi.fn((input: RequestInfo | URL) => {
      const url = typeof input === 'string' ? input : input.toString()
      if (url.includes('/api/v1/branding')) {
        return Promise.resolve(new Response(JSON.stringify(CONFIGURED), { status: 200 }))
      }
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
  stubBrandingFetch()
})

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('panel instance identity threading (FR-92)', () => {
  it('shows the configured name in the sidebar and updates the browser title once authenticated', async () => {
    window.localStorage.setItem('hikrad.access_token', 'access-abc')
    window.localStorage.setItem('hikrad.refresh_token', 'refresh-def')
    window.localStorage.setItem(
      'hikrad.manager',
      JSON.stringify({ id: 'm1', username: 'admin', role: 'admin' }),
    )

    renderAt('/')

    expect(await screen.findByText('Nur Net')).toBeInTheDocument()
    await waitFor(() => expect(document.title).toBe('Nur Net'))
  })

  it('shows the configured name on the login screen (pre-auth)', async () => {
    renderAt('/login')

    expect(await screen.findByText('Nur Net')).toBeInTheDocument()
  })
})
