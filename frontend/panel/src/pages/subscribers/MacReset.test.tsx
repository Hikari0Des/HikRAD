import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { I18nProvider } from '@hikrad/shared'
import en from '@hikrad/shared/locales/en/panel.json'

import { AuthProvider } from '../../auth/AuthContext'
import { tokenStore } from '../../auth/tokenStore'
import { ToastProvider } from '../../components/Toast'
import type { Subscriber, SubscriberDetail } from '../../api/types'
import { UserDetailPage } from './UserDetailPage'

// Keep the flow test focused on the reset-MAC interaction: stub the data layer.
const resetMac = vi.fn()
const getSubscriber = vi.fn()

vi.mock('../../api/subscribers', () => ({
  getSubscriber: (id: string) => getSubscriber(id),
  resetMac: (id: string) => resetMac(id),
}))
vi.mock('../../api/profiles', () => ({ listProfiles: () => Promise.resolve({ items: [] }) }))
vi.mock('../../api/managers', () => ({
  listManagers: () => Promise.reject(new Error('forbidden')),
}))
vi.mock('../../api/live', () => ({
  openLiveStream: () => ({ close: () => {} }),
  usageBySubscriber: () => Promise.resolve([]),
  disconnectSession: () => Promise.resolve({ outcome: 'ack' }),
  listSessionHistory: () => Promise.resolve({ items: [], next_cursor: null }),
}))

const subscriber: Subscriber = {
  id: 's1',
  username: 'ali',
  name: 'Ali',
  phone: null,
  address: null,
  notes: null,
  status: 'active',
  profile_id: null,
  owner_manager_id: null,
  expires_at: null,
  mac_lock_mode: 'learn',
  learned_mac: 'AA:BB:CC:DD:EE:FF',
  static_ip: null,
  session_limit_override: null,
  rate_override: null,
  price_override: null,
  disabled_reason: null,
  service_type: 'pppoe',
  nas_id: null,
  nas_service_id: null,
  whatsapp_opt_in: false,
  has_password: true,
  pending_profile_id: null,
  created_at: '2026-07-01T00:00:00Z',
  updated_at: '2026-07-01T00:00:00Z',
}

const detail: SubscriberDetail = {
  subscriber,
  profile: null,
  owner: null,
  live: { online: false, sessions: 0 },
  overrides: { rate: false, price: false, session_limit: false, static_ip: false },
  links: {},
}

function renderPage() {
  return render(
    <I18nProvider>
      <AuthProvider>
        <ToastProvider>
          <MemoryRouter initialEntries={['/subscribers/s1']}>
            <Routes>
              <Route path="/subscribers/:id" element={<UserDetailPage />} />
            </Routes>
          </MemoryRouter>
        </ToastProvider>
      </AuthProvider>
    </I18nProvider>,
  )
}

beforeEach(() => {
  resetMac.mockReset().mockResolvedValue({ ...subscriber, learned_mac: null })
  getSubscriber.mockReset().mockResolvedValue(detail)
  window.localStorage.clear()
  tokenStore.setManager({ id: 'm1', username: 'admin', role: 'admin' })
})

afterEach(() => {
  vi.clearAllMocks()
})

describe('Reset MAC flow (FR-5.2)', () => {
  it('confirms and calls the reset-mac endpoint', async () => {
    const user = userEvent.setup()
    renderPage()

    // Wait for the detail to load (the username heading appears).
    expect(await screen.findByRole('heading', { name: 'ali' })).toBeInTheDocument()

    // Open the confirm dialog from the header action.
    await user.click(screen.getByRole('button', { name: en.subscriber.resetMac }))
    // The dialog shows its confirmation body (unique to the dialog).
    expect(await screen.findByText(en.subscriber.resetMacBody)).toBeInTheDocument()

    // Confirm — the dialog's confirm button shares the "Reset MAC" label.
    const buttons = screen.getAllByRole('button', { name: en.subscriber.resetMac })
    await user.click(buttons[buttons.length - 1])

    await waitFor(() => expect(resetMac).toHaveBeenCalledWith('s1'))
  })
})
