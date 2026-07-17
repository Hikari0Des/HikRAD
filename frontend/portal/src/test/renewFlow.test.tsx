import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { I18nProvider } from '@hikrad/shared'

import en from '@hikrad/shared/locales/en/portal.json'
import { RenewPage } from '../pages/RenewPage'

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
  // Every RenewPage mount checks for a pending scratch-card status (contract
  // C8 seam) — default it to "none" unless a test overrides the first call.
  fetchMock.mockImplementation((url: string) => {
    if (url.includes('/portal/card-payments/mine')) {
      return Promise.resolve(jsonResponse(200, null))
    }
    return Promise.resolve(
      jsonResponse(404, { error: { code: 'not_found', message: 'unhandled' } }),
    )
  })
})

afterEach(() => {
  vi.unstubAllGlobals()
})

function renderRenew() {
  return render(
    <I18nProvider>
      <RenewPage />
    </I18nProvider>,
  )
}

describe('renew flow — voucher redeem states (FR-42, task 3)', () => {
  it('shows a success screen with the new expiry on redeem', async () => {
    fetchMock.mockImplementation((url: string) => {
      if (url.includes('/portal/card-payments/mine'))
        return Promise.resolve(jsonResponse(200, null))
      if (url.includes('/portal/vouchers/redeem')) {
        return Promise.resolve(
          jsonResponse(200, {
            ledger_tx_id: 't1',
            receipt_no: 'R1',
            new_expires_at: '2026-08-01T00:00:00Z',
            currency: 'IQD',
            coa_result: 'restored',
          }),
        )
      }
      return Promise.resolve(jsonResponse(404, {}))
    })

    const user = userEvent.setup()
    renderRenew()
    await user.type(screen.getByLabelText(en.portal.renew.voucher.label), 'ABCD1234')
    await user.click(screen.getByRole('button', { name: en.portal.renew.voucher.submit }))

    expect(await screen.findByText(en.portal.renew.voucher.successTitle)).toBeInTheDocument()
  })

  it.each([
    ['voucher_used', en.portal.renew.voucher.error.used],
    ['voucher_expired', en.portal.renew.voucher.error.expired],
    ['voucher_invalid', en.portal.renew.voucher.error.invalid],
  ])('shows a clear error for %s', async (code, expected) => {
    fetchMock.mockImplementation((url: string) => {
      if (url.includes('/portal/card-payments/mine'))
        return Promise.resolve(jsonResponse(200, null))
      if (url.includes('/portal/vouchers/redeem')) {
        return Promise.resolve(jsonResponse(422, { error: { code, message: code } }))
      }
      return Promise.resolve(jsonResponse(404, {}))
    })

    const user = userEvent.setup()
    renderRenew()
    await user.type(screen.getByLabelText(en.portal.renew.voucher.label), 'BADCODE')
    await user.click(screen.getByRole('button', { name: en.portal.renew.voucher.submit }))

    expect(await screen.findByText(expected)).toBeInTheDocument()
  })

  it('degrades to an explanatory message with the voucher path prominent when all gateways are down (NFR-7)', async () => {
    fetchMock.mockImplementation((url: string) => {
      if (url.includes('/portal/card-payments/mine'))
        return Promise.resolve(jsonResponse(200, null))
      if (url.includes('/portal/payments/gateways')) {
        return Promise.resolve(jsonResponse(200, { items: [] }))
      }
      return Promise.resolve(jsonResponse(404, {}))
    })

    const user = userEvent.setup()
    renderRenew()
    await user.click(screen.getByRole('tab', { name: en.portal.renew.tab.gateway }))

    expect(await screen.findByText(en.portal.renew.gateway.allDownTitle)).toBeInTheDocument()

    // The escape hatch switches back to the voucher tab.
    await user.click(screen.getByRole('button', { name: en.portal.renew.gateway.useVoucher }))
    await waitFor(() =>
      expect(screen.getByRole('tab', { name: en.portal.renew.tab.voucher })).toHaveAttribute(
        'aria-selected',
        'true',
      ),
    )
  })

  it('lists enabled gateways and starts a payment on tap', async () => {
    fetchMock.mockImplementation((url: string) => {
      if (url.includes('/portal/card-payments/mine'))
        return Promise.resolve(jsonResponse(200, null))
      if (url.includes('/portal/payments/gateways')) {
        return Promise.resolve(jsonResponse(200, { items: [{ id: 'mock', name: 'Mock Wallet' }] }))
      }
      if (url.includes('/portal/payments/mock/create')) {
        return Promise.resolve(
          jsonResponse(200, { redirect_url: 'https://pay.test/x', intent_id: 'i1' }),
        )
      }
      return Promise.resolve(jsonResponse(404, {}))
    })

    // jsdom doesn't implement navigation; stub the setter so the redirect is observable.
    Object.defineProperty(window, 'location', {
      configurable: true,
      value: { ...window.location, href: '' },
    })

    const user = userEvent.setup()
    renderRenew()
    await user.click(screen.getByRole('tab', { name: en.portal.renew.tab.gateway }))
    await user.click(await screen.findByRole('button', { name: /Mock Wallet/ }))

    await waitFor(() => expect(window.location.href).toBe('https://pay.test/x'))
    expect(window.localStorage.getItem('hikrad.portal.pending_payment_intent')).toBe(
      JSON.stringify({ gateway: 'mock', intentId: 'i1' }),
    )
  })
})
