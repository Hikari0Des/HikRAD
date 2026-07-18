import { render, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { I18nProvider } from '@hikrad/shared'

import { AuthProvider } from '../auth/AuthContext'
import { DashboardPage } from './DashboardPage'

vi.mock('../api/monitoring', () => ({
  getDashboard: () =>
    Promise.resolve({
      online_now: 3,
      online_24h_sparkline: [],
      subs: { active: 10, expired: 2, expiring_7d: 1 },
      revenue_today_iqd: 50000,
      nas_cards: [{ id: 'n1', name: 'Core NAS', status: 'up', latency_ms: 5 }],
      radius_rps: 2,
      pipeline: { invariant_ok: true, depth: 0 },
      my_balance: [{ currency: 'IQD', balance: 1000 }],
      pending_payment_tickets: 0,
      alerts_feed: [],
    }),
}))
vi.mock('../api/preferences', () => ({
  getPreferences: () => Promise.resolve({}),
  putPreferences: () => Promise.resolve({}),
}))

function seedAdminSession() {
  window.localStorage.setItem('hikrad.access_token', 'access-abc')
  window.localStorage.setItem('hikrad.refresh_token', 'refresh-def')
  window.localStorage.setItem(
    'hikrad.manager',
    JSON.stringify({ id: 'm1', username: 'admin', role: 'admin' }),
  )
}

beforeEach(() => {
  window.localStorage.clear()
  seedAdminSession()
})

/**
 * FR-90.3: the dashboard grid is phone-first single column unconditionally —
 * it may only ever GAIN columns at sm/lg breakpoints, and a 2x-sized widget
 * may only ever span extra columns via a responsive-prefixed class, never an
 * unprefixed one that would also apply on a phone. jsdom doesn't evaluate
 * media queries, so this asserts the structural contract (which Tailwind
 * classes are present) rather than a computed pixel width — the same
 * DOM-structure assertion style rtl-smoke.test.tsx already uses.
 */
describe('DashboardPage mobile-first layout (FR-90.3)', () => {
  it('renders a single-column-by-default grid with only responsive-prefixed column spans', async () => {
    const { container } = render(
      <I18nProvider>
        <MemoryRouter>
          <AuthProvider>
            <DashboardPage />
          </AuthProvider>
        </MemoryRouter>
      </I18nProvider>,
    )

    await waitFor(() => expect(container.querySelector('.grid')).not.toBeNull())

    const grid = container.querySelector('.grid') as HTMLElement
    expect(grid.className).toContain('grid-cols-1')
    // Extra columns are only ever added by responsive-prefixed classes.
    expect(grid.className).not.toMatch(/(?:^|\s)grid-cols-[23](?:\s|$)/)

    // nas-health defaults to 2x (contract C1) and is permitted for the admin
    // role — its wrapper must carry only the responsive span class.
    const spanned = container.querySelectorAll('[class*="col-span-2"]')
    expect(spanned.length).toBeGreaterThan(0)
    spanned.forEach((el) => {
      expect(el.className).toContain('sm:col-span-2')
      expect(el.className).not.toMatch(/(?:^|\s)col-span-2(?:\s|$)/)
    })
  })
})
