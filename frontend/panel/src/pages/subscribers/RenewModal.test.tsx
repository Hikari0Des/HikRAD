import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { I18nProvider } from '@hikrad/shared'
import en from '@hikrad/shared/locales/en/panel.json'

import { ApiError } from '../../api/client'
import type { Profile, Subscriber } from '../../api/types'
import { RenewModal } from './RenewModal'

const renewSubscriber = vi.fn()
const printReceipt = vi.fn()

vi.mock('../../api/billing', async () => {
  const actual = await vi.importActual<typeof import('../../api/billing')>('../../api/billing')
  return {
    ...actual,
    renewSubscriber: (...args: unknown[]) => renewSubscriber(...args),
    printReceipt: (...args: unknown[]) => printReceipt(...args),
  }
})

const profile: Profile = {
  id: 'p1',
  name: 'Home 10M',
  price_iqd: 15000,
  duration_days: 30,
  rate_down_kbps: 10000,
  rate_up_kbps: 10000,
  pool_id: null,
  session_limit_default: 1,
  quota_mode: 'unlimited',
  quota_total_bytes: null,
  quota_down_bytes: null,
  quota_up_bytes: null,
  throttle_rate: null,
  expiry_behavior: 'expired_pool',
  quota_behavior: 'block',
  hotspot_rate_down_kbps: null,
  hotspot_rate_up_kbps: null,
  archived: false,
  created_at: '2026-07-01T00:00:00Z',
  updated_at: '2026-07-01T00:00:00Z',
}

const subscriber: Subscriber = {
  id: 's1',
  username: 'ali',
  name: 'Ali',
  phone: null,
  address: null,
  notes: null,
  status: 'expired',
  profile_id: 'p1',
  owner_manager_id: null,
  expires_at: '2026-07-01T00:00:00Z',
  mac_lock_mode: 'off',
  learned_mac: null,
  static_ip: null,
  session_limit_override: null,
  rate_override: null,
  price_override: null,
  disabled_reason: null,
  allow_hotspot: false,
  whatsapp_opt_in: false,
  has_password: true,
  pending_profile_id: null,
  created_at: '2026-07-01T00:00:00Z',
  updated_at: '2026-07-01T00:00:00Z',
}

const onRenewed = vi.fn()

function renderModal() {
  return render(
    <I18nProvider>
      <RenewModal
        open
        onOpenChange={() => {}}
        subscriber={subscriber}
        currentProfileId="p1"
        profiles={[profile]}
        onRenewed={onRenewed}
      />
    </I18nProvider>,
  )
}

beforeEach(() => {
  renewSubscriber.mockReset()
  printReceipt.mockReset().mockResolvedValue(undefined)
  onRenewed.mockReset()
})

afterEach(() => vi.clearAllMocks())

describe('RenewModal (FR-19)', () => {
  it('confirms and shows the success state with the CoA outcome + receipt actions', async () => {
    const user = userEvent.setup()
    renewSubscriber.mockResolvedValue({
      ledger_tx_id: 'tx1',
      receipt_no: 'R-100',
      new_expires_at: '2026-08-01T00:00:00Z',
      price_iqd: 15000,
      coa_result: 'restored',
    })
    renderModal()

    await user.click(screen.getByRole('button', { name: en.renew.confirm }))

    expect(await screen.findByText(en.renew.success)).toBeInTheDocument()
    expect(screen.getByText(en.renew.coaResult.restored)).toBeInTheDocument()
    expect(screen.getByText('R-100')).toBeInTheDocument()
    // Idempotency key was sent on the request.
    const [, , idemKey] = renewSubscriber.mock.calls[0]
    expect(idemKey).toBeTruthy()
    expect(onRenewed).toHaveBeenCalled()

    // Receipt print action fires with the chosen language.
    await user.click(screen.getByRole('button', { name: en.renew.printAr }))
    await waitFor(() => expect(printReceipt).toHaveBeenCalledWith('R-100', 'ar'))
  })

  it('renders the disconnect-fallback CoA badge', async () => {
    const user = userEvent.setup()
    renewSubscriber.mockResolvedValue({
      ledger_tx_id: 'tx1',
      receipt_no: 'R-101',
      new_expires_at: '2026-08-01T00:00:00Z',
      price_iqd: 15000,
      coa_result: 'disconnect_fallback',
    })
    renderModal()

    await user.click(screen.getByRole('button', { name: en.renew.confirm }))
    expect(await screen.findByText(en.renew.coaResult.disconnect_fallback)).toBeInTheDocument()
  })

  it('surfaces the insufficient-balance error with a hint and stays on the form', async () => {
    const user = userEvent.setup()
    renewSubscriber.mockRejectedValue(
      new ApiError(422, 'insufficient_balance', 'billing.error.insufficient_balance'),
    )
    renderModal()

    await user.click(screen.getByRole('button', { name: en.renew.confirm }))

    expect(await screen.findByText(en.renew.error.insufficient_balance)).toBeInTheDocument()
    expect(screen.getByText(en.renew.error.insufficient_balance_hint)).toBeInTheDocument()
    // Still on the form — the confirm button is present, no success state.
    expect(screen.getByRole('button', { name: en.renew.confirm })).toBeInTheDocument()
    expect(screen.queryByText(en.renew.success)).not.toBeInTheDocument()
  })

  it('refreshes parent state when the profile was archived mid-open', async () => {
    const user = userEvent.setup()
    renewSubscriber.mockRejectedValue(
      new ApiError(422, 'profile_archived', 'the selected profile is archived'),
    )
    renderModal()

    await user.click(screen.getByRole('button', { name: en.renew.confirm }))

    expect(await screen.findByText(en.renew.error.profile_archived)).toBeInTheDocument()
    await waitFor(() => expect(onRenewed).toHaveBeenCalled())
  })
})
