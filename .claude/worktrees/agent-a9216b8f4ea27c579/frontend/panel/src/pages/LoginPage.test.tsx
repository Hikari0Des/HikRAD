import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { I18nProvider } from '@hikrad/shared'

import { AuthProvider } from '../auth/AuthContext'
import en from '@hikrad/shared/locales/en/panel.json'
import { LoginPage } from './LoginPage'

const fetchMock = vi.fn()

function jsonResponse(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

function renderLogin() {
  return render(
    <I18nProvider>
      <MemoryRouter initialEntries={['/login']}>
        <AuthProvider>
          <Routes>
            <Route path="/login" element={<LoginPage />} />
            <Route path="/" element={<div data-testid="dashboard" />} />
          </Routes>
        </AuthProvider>
      </MemoryRouter>
    </I18nProvider>,
  )
}

async function submitCredentials(username: string, password: string) {
  const user = userEvent.setup()
  await user.type(screen.getByLabelText(en.login.username), username)
  await user.type(screen.getByLabelText(en.login.password), password)
  await user.click(screen.getByRole('button', { name: en.login.submit }))
  return user
}

beforeEach(() => {
  fetchMock.mockReset()
  vi.stubGlobal('fetch', fetchMock)
  window.localStorage.clear()
})

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('LoginPage', () => {
  it('logs in against the C7 stub, stores tokens and navigates to the dashboard', async () => {
    fetchMock.mockResolvedValueOnce(
      jsonResponse(200, {
        access_token: 'access-abc',
        refresh_token: 'refresh-def',
        manager: { id: 'm1', username: 'admin', role: 'admin' },
      }),
    )

    renderLogin()
    await submitCredentials('admin', 'admin')

    expect(await screen.findByTestId('dashboard')).toBeInTheDocument()
    expect(window.localStorage.getItem('hikrad.access_token')).toBe('access-abc')
    expect(window.localStorage.getItem('hikrad.refresh_token')).toBe('refresh-def')

    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit]
    expect(url).toBe('/api/v1/auth/login')
    expect(JSON.parse(init.body as string)).toEqual({ username: 'admin', password: 'admin' })
  })

  it('shows a localized invalid-credentials error on 401 and stays on the login page', async () => {
    fetchMock.mockResolvedValueOnce(
      jsonResponse(401, { error: { code: 'invalid_credentials', message: 'nope' } }),
    )

    renderLogin()
    await submitCredentials('admin', 'wrong')

    const alert = await screen.findByRole('alert')
    expect(alert).toHaveTextContent(en.login.error.invalid_credentials)
    expect(screen.queryByTestId('dashboard')).not.toBeInTheDocument()
    expect(window.localStorage.getItem('hikrad.access_token')).toBeNull()
  })

  it('shows a friendly localized error when the backend is unreachable', async () => {
    fetchMock.mockRejectedValueOnce(new TypeError('fetch failed'))

    renderLogin()
    await submitCredentials('admin', 'admin')

    const alert = await screen.findByRole('alert')
    expect(alert).toHaveTextContent(en.login.error.network)
    expect(screen.queryByTestId('dashboard')).not.toBeInTheDocument()
  })

  it('prompts for a TOTP code on totp_required, then logs in with the code (FR-28.1)', async () => {
    // First submit → 401 totp_required; second submit (with code) → session.
    fetchMock
      .mockResolvedValueOnce(
        jsonResponse(401, { error: { code: 'totp_required', message: 'code required' } }),
      )
      .mockResolvedValueOnce(
        jsonResponse(200, {
          access_token: 'access-2fa',
          refresh_token: 'refresh-2fa',
          manager: { id: 'm1', username: 'admin', role: 'admin' },
        }),
      )

    renderLogin()
    const user = await submitCredentials('admin', 'admin')

    // The code entry step appears.
    const codeInput = await screen.findByLabelText(en.login.totpCode)
    await user.type(codeInput, '123456')
    await user.click(screen.getByRole('button', { name: en.login.verifyCode }))

    expect(await screen.findByTestId('dashboard')).toBeInTheDocument()
    // The second request carried the code.
    const [, init] = fetchMock.mock.calls[1] as [string, RequestInit]
    expect(JSON.parse(init.body as string)).toEqual({
      username: 'admin',
      password: 'admin',
      totp_code: '123456',
    })
  })

  it('shows an invalid-code error when the TOTP code is rejected', async () => {
    fetchMock
      .mockResolvedValueOnce(
        jsonResponse(401, { error: { code: 'totp_required', message: 'code required' } }),
      )
      .mockResolvedValueOnce(
        jsonResponse(401, { error: { code: 'totp_required', message: 'bad code' } }),
      )

    renderLogin()
    const user = await submitCredentials('admin', 'admin')
    const codeInput = await screen.findByLabelText(en.login.totpCode)
    await user.type(codeInput, '000000')
    await user.click(screen.getByRole('button', { name: en.login.verifyCode }))

    const alert = await screen.findByRole('alert')
    expect(alert).toHaveTextContent(en.login.error.invalid_totp)
    expect(screen.queryByTestId('dashboard')).not.toBeInTheDocument()
  })

  it('drives forced enrolment when 2FA is required but not set up', async () => {
    fetchMock
      .mockResolvedValueOnce(
        jsonResponse(200, { totp_enrollment_required: true, enrollment_token: 'enroll-tok' }),
      )
      // TotpEnroll mounts and requests an enrolment secret.
      .mockResolvedValueOnce(
        jsonResponse(200, { otpauth_uri: 'otpauth://totp/x', secret: 'ABCDEF123456' }),
      )

    renderLogin()
    await submitCredentials('admin', 'admin')

    expect(await screen.findByText(en.login.enrollRequired)).toBeInTheDocument()
    // The manual setup key is shown for entry.
    expect(await screen.findByText('ABCDEF123456')).toBeInTheDocument()
  })
})
