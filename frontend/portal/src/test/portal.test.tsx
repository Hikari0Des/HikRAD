import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import { describe, expect, it } from 'vitest'

import { I18nProvider } from '@hikrad/shared'

import { App } from '../App'
import { AuthProvider } from '../auth/AuthContext'
import { BrandingProvider } from '../branding'

import ar from '../../../shared/locales/ar/portal.json'
import en from '../../../shared/locales/en/portal.json'
import ku from '../../../shared/locales/ku/portal.json'

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

describe('portal shell (gate item 4: trilingual + RTL)', () => {
  it('renders the login screen in all three locales with correct dir', async () => {
    const user = userEvent.setup()
    renderPortal()

    // en (default) — the FR-167 auto-login probe runs silently on mount
    // (and fails fast in this test environment, no live session to find),
    // so the login form appears once that settles, not synchronously.
    expect(await screen.findByText(en.portal.login.title)).toBeInTheDocument()
    expect(document.documentElement.dir).toBe('ltr')

    // ar — flips RTL
    await user.click(screen.getByRole('button', { name: 'العربية' }))
    expect(screen.getByText(ar.portal.login.title)).toBeInTheDocument()
    expect(document.documentElement.dir).toBe('rtl')
    expect(document.documentElement.lang).toBe('ar')

    // ku — RTL as well, its own strings (never ar)
    await user.click(screen.getByRole('button', { name: 'کوردی' }))
    expect(screen.getByText(ku.portal.login.title)).toBeInTheDocument()
    expect(document.documentElement.dir).toBe('rtl')
    expect(document.documentElement.lang).toBe('ku')
  })

  it('keeps app state (typed input) across a locale switch', async () => {
    const user = userEvent.setup()
    renderPortal()

    const username = await screen.findByLabelText(en.portal.login.username)
    await user.type(username, 'noor01')
    await user.click(screen.getByRole('button', { name: 'العربية' }))

    expect(screen.getByLabelText(ar.portal.login.username)).toHaveValue('noor01')
  })

  it('redirects an unauthenticated subscriber away from protected routes (IDOR/session gate)', async () => {
    renderPortal('/home')
    // <RequireAuth> sends anyone without a session back to the login screen
    // — after the auto-login probe (FR-167) settles, since a device with no
    // live session must also fall through to a normal login, not a blank
    // screen forever.
    expect(await screen.findByText(en.portal.login.title)).toBeInTheDocument()
  })
})
