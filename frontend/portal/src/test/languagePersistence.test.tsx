import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { I18nProvider } from '@hikrad/shared'

import { AuthProvider } from '../auth/AuthContext'
import { tokenStore } from '../auth/tokenStore'
import { LanguageSwitcher } from '../components/LanguageSwitcher'

function jsonResponse(status: number, body: unknown): Response {
  // A 204 must have a null body (Fetch spec forbids a body on that status).
  const payload = status === 204 ? null : JSON.stringify(body)
  return new Response(payload, { status, headers: { 'Content-Type': 'application/json' } })
}

const fetchMock = vi.fn()

beforeEach(() => {
  fetchMock.mockReset()
  fetchMock.mockResolvedValue(jsonResponse(204, null))
  vi.stubGlobal('fetch', fetchMock)
  window.localStorage.clear()
})

afterEach(() => {
  vi.unstubAllGlobals()
})

function renderSwitcher() {
  return render(
    <I18nProvider>
      <AuthProvider>
        <LanguageSwitcher />
      </AuthProvider>
    </I18nProvider>,
  )
}

describe('language persistence (FR-43, task 4)', () => {
  it('persists the choice to localStorage even when signed out, without calling the API', async () => {
    const user = userEvent.setup()
    renderSwitcher()

    await user.click(screen.getByRole('button', { name: 'العربية' }))

    expect(window.localStorage.getItem('hikrad.locale')).toBe('ar')
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('also persists server-side via PUT /portal/language when a subscriber is signed in', async () => {
    tokenStore.setTokens('access-1', 'refresh-1')
    tokenStore.setSubscriber({ id: 's1', username: 'noor01', name: 'Noor', language: 'en' })

    const user = userEvent.setup()
    renderSwitcher()

    await user.click(screen.getByRole('button', { name: 'کوردی' }))

    expect(window.localStorage.getItem('hikrad.locale')).toBe('ku')
    expect(fetchMock).toHaveBeenCalledWith(
      expect.stringContaining('/portal/language'),
      expect.objectContaining({ method: 'PUT' }),
    )
    const [, init] = fetchMock.mock.calls[0] as [string, RequestInit]
    expect(JSON.parse(init.body as string)).toEqual({ language: 'ku' })
  })

  it('survives a reload by re-reading the persisted locale on next mount', async () => {
    const user = userEvent.setup()
    const first = renderSwitcher()
    await user.click(screen.getByRole('button', { name: 'العربية' }))
    first.unmount()

    renderSwitcher()
    expect(screen.getByRole('button', { name: 'العربية' })).toHaveAttribute('aria-pressed', 'true')
  })
})
