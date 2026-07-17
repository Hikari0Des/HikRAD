import { render, screen } from '@testing-library/react'
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
  // Every RenewPage mount resolves the unified Pay screen's tile list
  // (contract C4) — default it to "voucher only" unless a test overrides.
  fetchMock.mockImplementation((url: string) => {
    if (url.includes('/portal/pay-methods')) {
      return Promise.resolve(jsonResponse(200, { items: [{ key: 'voucher', kind: 'voucher' }] }))
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

describe('renew flow — unified Pay screen (v2-2, FR-42/78)', () => {
  it('opens the voucher tile and shows a success screen with the new expiry on redeem', async () => {
    fetchMock.mockImplementation((url: string) => {
      if (url.includes('/portal/pay-methods')) {
        return Promise.resolve(jsonResponse(200, { items: [{ key: 'voucher', kind: 'voucher' }] }))
      }
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
    await user.click(await screen.findByRole('button', { name: en.portal.renew.tab.voucher }))
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
      if (url.includes('/portal/pay-methods')) {
        return Promise.resolve(jsonResponse(200, { items: [{ key: 'voucher', kind: 'voucher' }] }))
      }
      if (url.includes('/portal/vouchers/redeem')) {
        return Promise.resolve(jsonResponse(422, { error: { code, message: code } }))
      }
      return Promise.resolve(jsonResponse(404, {}))
    })

    const user = userEvent.setup()
    renderRenew()
    await user.click(await screen.findByRole('button', { name: en.portal.renew.tab.voucher }))
    await user.type(screen.getByLabelText(en.portal.renew.voucher.label), 'BADCODE')
    await user.click(screen.getByRole('button', { name: en.portal.renew.voucher.submit }))

    expect(await screen.findByText(expected)).toBeInTheDocument()
  })

  it('degrades to an explanatory message when no payment methods are enabled (NFR-7, kickoff blocker 1)', async () => {
    fetchMock.mockImplementation((url: string) => {
      if (url.includes('/portal/pay-methods')) {
        return Promise.resolve(jsonResponse(200, { items: [] }))
      }
      return Promise.resolve(jsonResponse(404, {}))
    })

    renderRenew()

    expect(await screen.findByText(en.portal.renew.pay.noneTitle)).toBeInTheDocument()
  })

  it('lists a provider tile and submits a transfer-proof ticket on tap', async () => {
    fetchMock.mockImplementation((url: string) => {
      if (url.includes('/portal/pay-methods')) {
        return Promise.resolve(
          jsonResponse(200, {
            items: [
              {
                key: 'prov-1',
                kind: 'provider',
                provider_name: 'Zain Cash',
                account_details: '0770 000 0000',
                instructions_text: 'Send and enter the reference.',
              },
            ],
          }),
        )
      }
      if (url.includes('/portal/payment-tickets')) {
        return Promise.resolve(
          jsonResponse(200, {
            id: 'tk1',
            state: 'pending',
            trial_granted: true,
            trial_expires_at: '2026-08-01T00:00:00Z',
          }),
        )
      }
      return Promise.resolve(jsonResponse(404, {}))
    })

    const user = userEvent.setup()
    renderRenew()
    await user.click(await screen.findByRole('button', { name: 'Zain Cash' }))
    await user.type(screen.getByLabelText(en.portal.renew.pay.referenceLabel), 'REF123')
    await user.click(screen.getByRole('button', { name: en.portal.renew.pay.submit }))

    expect(await screen.findByText(en.portal.renew.pay.submittedTitle)).toBeInTheDocument()
  })
})
