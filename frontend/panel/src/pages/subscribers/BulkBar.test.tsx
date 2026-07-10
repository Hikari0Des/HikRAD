import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { I18nProvider } from '@hikrad/shared'
import en from '@hikrad/shared/locales/en/panel.json'

import { AuthProvider } from '../../auth/AuthContext'
import { tokenStore } from '../../auth/tokenStore'
import { ToastProvider } from '../../components/Toast'
import { BulkBar } from './BulkBar'

const fetchMock = vi.fn()

function json(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

function renderBar() {
  return render(
    <I18nProvider>
      <AuthProvider>
        <ToastProvider>
          <BulkBar filter={{ status: 'active' }} profiles={[]} managers={[]} onDone={() => {}} />
        </ToastProvider>
      </AuthProvider>
    </I18nProvider>,
  )
}

beforeEach(() => {
  fetchMock.mockReset()
  vi.stubGlobal('fetch', fetchMock)
  window.localStorage.clear()
})

afterEach(() => {
  vi.unstubAllGlobals()
  vi.useRealTimers()
})

describe('BulkBar (FR-4)', () => {
  it('polls the async job and renders per-row failures', async () => {
    tokenStore.setManager({ id: 'm1', username: 'admin', role: 'admin' })
    const running = {
      id: 'j1',
      action: 'enable',
      status: 'running',
      total: 2,
      done: 0,
      succeeded: 0,
      failed: 0,
      failures: [],
      started_at: '2026-07-10T10:00:00Z',
    }
    const completed = {
      ...running,
      status: 'completed',
      done: 2,
      succeeded: 1,
      failed: 1,
      failures: [{ subscriber_id: 's1', username: 'bob', error: 'boom' }],
    }
    fetchMock.mockImplementation((url: string, init?: RequestInit) => {
      if ((init?.method ?? 'GET') === 'POST' && url.endsWith('/subscribers/bulk')) {
        return Promise.resolve(json(202, running))
      }
      if (url.includes('/subscribers/bulk/j1')) {
        return Promise.resolve(json(200, completed))
      }
      throw new Error(`unexpected fetch ${url}`)
    })

    renderBar()

    fireEvent.click(screen.getByText(en.bulk.enable))

    // The POST starts the job, then a 700ms poll returns the completed job with
    // the per-row failure. waitFor drives the interval on real timers.
    await waitFor(() => expect(screen.getByText('bob')).toBeInTheDocument(), { timeout: 3000 })
    expect(screen.getByText('boom', { exact: false })).toBeInTheDocument()
  })

  it('hides mutating + export actions from an agent (permission gating)', () => {
    tokenStore.setManager({ id: 'a1', username: 'hassan', role: 'agent' })
    renderBar()
    // Agents hold neither subscribers.edit nor export.
    expect(screen.queryByText(en.bulk.enable)).not.toBeInTheDocument()
    expect(screen.queryByText(en.bulk.disable)).not.toBeInTheDocument()
    expect(screen.queryByText(en.bulk.export)).not.toBeInTheDocument()
  })
})
