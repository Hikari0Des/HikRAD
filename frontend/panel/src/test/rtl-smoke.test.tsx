import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it } from 'vitest'

import { I18nProvider } from '@hikrad/shared'

import { App } from '../App'
import { AuthProvider } from '../auth/AuthContext'
import ar from '@hikrad/shared/locales/ar/panel.json'
import en from '@hikrad/shared/locales/en/panel.json'

function seedSession() {
  window.localStorage.setItem('hikrad.access_token', 'access-abc')
  window.localStorage.setItem('hikrad.refresh_token', 'refresh-def')
  window.localStorage.setItem(
    'hikrad.manager',
    JSON.stringify({ id: 'm1', username: 'admin', role: 'admin' }),
  )
}

function renderAppAt(path: string) {
  return render(
    <I18nProvider>
      <MemoryRouter initialEntries={[path]}>
        <AuthProvider>
          <App />
        </AuthProvider>
      </MemoryRouter>
    </I18nProvider>,
  )
}

beforeEach(() => {
  window.localStorage.clear()
  seedSession()
})

describe('RTL smoke', () => {
  it('renders the shell mirrored in Arabic: dir/lang set, nav localized, machine values isolated LTR', async () => {
    window.localStorage.setItem('hikrad.locale', 'ar')

    const { container } = renderAppAt('/dev/rtl-smoke')

    await waitFor(() => expect(document.documentElement.dir).toBe('rtl'))
    expect(document.documentElement.lang).toBe('ar')

    // Sidebar nav is localized (no hardcoded strings).
    expect(screen.getByText(ar.nav.dashboard)).toBeInTheDocument()

    // Usernames / MACs / IPs stay LTR inside the RTL page via the shared
    // bidi-isolate component.
    const isolated = container.querySelectorAll('[data-testid="bidi-isolated"] bdi[dir="ltr"]')
    expect(isolated.length).toBeGreaterThanOrEqual(3)
  })

  it('flips the whole document en↔ar through the language switcher', async () => {
    window.localStorage.setItem('hikrad.locale', 'en')
    const user = userEvent.setup()

    renderAppAt('/dev/rtl-smoke')

    await waitFor(() => expect(document.documentElement.dir).toBe('ltr'))

    await user.click(screen.getByRole('button', { name: en.languages.ar }))
    await waitFor(() => expect(document.documentElement.dir).toBe('rtl'))
    expect(document.documentElement.lang).toBe('ar')
    expect(window.localStorage.getItem('hikrad.locale')).toBe('ar')

    await user.click(screen.getByRole('button', { name: en.languages.en }))
    await waitFor(() => expect(document.documentElement.dir).toBe('ltr'))
    expect(document.documentElement.lang).toBe('en')
  })

  it('redirects to /login when no session exists', async () => {
    window.localStorage.clear()

    renderAppAt('/')

    expect(await screen.findByRole('button', { name: en.login.submit })).toBeInTheDocument()
  })
})
