import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import { describe, expect, it } from 'vitest'

import { I18nProvider } from '@hikrad/shared'

import { App } from '../App'

import ar from '../../../shared/locales/ar/portal.json'
import en from '../../../shared/locales/en/portal.json'
import ku from '../../../shared/locales/ku/portal.json'

function renderPortal(route = '/') {
  return render(
    <I18nProvider>
      <MemoryRouter initialEntries={[route]}>
        <App />
      </MemoryRouter>
    </I18nProvider>,
  )
}

describe('portal skeleton (gate item 4)', () => {
  it('renders the login shell in all three locales with correct dir', async () => {
    const user = userEvent.setup()
    renderPortal()

    // en (default)
    expect(screen.getByText(en.portal.login.title)).toBeInTheDocument()
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

    const username = screen.getByLabelText(en.portal.login.username)
    await user.type(username, 'noor01')
    await user.click(screen.getByRole('button', { name: 'العربية' }))

    expect(screen.getByLabelText(ar.portal.login.username)).toHaveValue('noor01')
  })

  it('stubs the three routes with localized content and bottom navigation', async () => {
    const user = userEvent.setup()
    renderPortal('/home')

    expect(screen.getByText(en.portal.home.title)).toBeInTheDocument()

    await user.click(screen.getByRole('link', { name: en.portal.nav.usage }))
    expect(screen.getByText(en.portal.usage.chartTitle)).toBeInTheDocument()

    await user.click(screen.getByRole('link', { name: en.portal.nav.renew }))
    expect(screen.getByText(en.portal.renew.price)).toBeInTheDocument()
  })

  it('keeps mixed RTL sentences bidi-safe (numbers/usernames isolated)', async () => {
    const user = userEvent.setup()
    renderPortal('/usage')

    await user.click(screen.getByRole('button', { name: 'العربية' }))
    const sample = ar.portal.usage.mixedSample
      .replace('{username}', '⁨noor01⁩')
      .replace('{gb}', '⁨4.2⁩')
    expect(screen.getByText(sample)).toBeInTheDocument()
  })
})
