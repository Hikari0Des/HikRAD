import { render, screen } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { I18nProvider } from '@hikrad/shared'

import en from '@hikrad/shared/locales/en/portal.json'
import { PaymentReturnPage } from '../pages/PaymentReturnPage'
import { setPendingIntent } from '../lib/pendingPayment'

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
  // shouldAdvanceTime: RTL's findBy*/waitFor poll via setTimeout internally —
  // without this they'd hang forever once real timers are faked out from
  // under them.
  vi.useFakeTimers({ shouldAdvanceTime: true })
  window.localStorage.clear()
})

afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

function renderReturn(route: string) {
  return render(
    <I18nProvider>
      <MemoryRouter initialEntries={[route]}>
        <Routes>
          <Route path="/renew/return/:gateway" element={<PaymentReturnPage />} />
        </Routes>
      </MemoryRouter>
    </I18nProvider>,
  )
}

describe('payment intent polling (contract C3, task 3)', () => {
  it('polls while pending and moves to the success screen once confirmed', async () => {
    fetchMock
      .mockResolvedValueOnce(
        jsonResponse(200, {
          id: 'i1',
          gateway: 'mock',
          state: 'pending',
          amount_iqd: 25000,
          gateway_ref: 'REF-1',
        }),
      )
      .mockResolvedValueOnce(
        jsonResponse(200, {
          id: 'i1',
          gateway: 'mock',
          state: 'renewed',
          amount_iqd: 25000,
          gateway_ref: 'REF-1',
          new_expires_at: '2026-08-01T00:00:00Z',
        }),
      )

    renderReturn('/renew/return/mock?intent=i1')

    expect(await screen.findByText(en.portal.renew.return.pendingTitle)).toBeInTheDocument()
    expect(screen.getByText('Reference: REF-1')).toBeInTheDocument()

    await vi.advanceTimersByTimeAsync(3000)

    expect(await screen.findByText(en.portal.renew.return.successTitle)).toBeInTheDocument()
    // The resolved intent clears the persisted resume pointer.
    expect(window.localStorage.getItem('hikrad.portal.pending_payment_intent')).toBeNull()
  })

  it('shows the failed screen with a retry affordance on a terminal failure', async () => {
    fetchMock.mockResolvedValueOnce(
      jsonResponse(200, {
        id: 'i2',
        gateway: 'mock',
        state: 'failed',
        amount_iqd: 25000,
        gateway_ref: 'REF-2',
      }),
    )

    renderReturn('/renew/return/mock?intent=i2')

    expect(await screen.findByText(en.portal.renew.return.failedTitle)).toBeInTheDocument()
    expect(screen.getByRole('link', { name: en.portal.renew.return.retry })).toBeInTheDocument()
  })

  it('resumes polling a persisted intent when reopened without a query param (deep-link safe)', async () => {
    setPendingIntent({ gateway: 'mock', intentId: 'i3' })
    fetchMock.mockResolvedValueOnce(
      jsonResponse(200, {
        id: 'i3',
        gateway: 'mock',
        state: 'pending',
        amount_iqd: 25000,
        gateway_ref: 'REF-3',
      }),
    )

    renderReturn('/renew/return/mock')

    expect(await screen.findByText(en.portal.renew.return.pendingTitle)).toBeInTheDocument()
    expect(fetchMock).toHaveBeenCalledWith(
      expect.stringContaining('/portal/payments/intents/i3'),
      expect.anything(),
    )
  })
})
