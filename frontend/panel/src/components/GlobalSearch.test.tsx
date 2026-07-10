import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { I18nProvider } from '@hikrad/shared'
import en from '@hikrad/shared/locales/en/panel.json'

import { GlobalSearch } from './GlobalSearch'

const fetchMock = vi.fn()

function jsonResponse(body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: { 'Content-Type': 'application/json' },
  })
}

function renderSearch() {
  return render(
    <I18nProvider>
      <MemoryRouter>
        <GlobalSearch />
      </MemoryRouter>
    </I18nProvider>,
  )
}

beforeEach(() => {
  fetchMock.mockReset()
  fetchMock.mockResolvedValue(
    jsonResponse({
      items: [
        {
          type: 'subscriber',
          id: 's1',
          username: 'ali',
          name: 'Ali',
          phone: null,
          status: 'active',
        },
      ],
    }),
  )
  vi.stubGlobal('fetch', fetchMock)
})

afterEach(() => {
  vi.unstubAllGlobals()
  vi.useRealTimers()
})

describe('GlobalSearch (FR-2)', () => {
  it("focuses the input on the '/' shortcut from elsewhere on the page", async () => {
    const user = userEvent.setup()
    renderSearch()
    const input = screen.getByLabelText(en.search.label)
    expect(input).not.toHaveFocus()
    await user.keyboard('/')
    expect(input).toHaveFocus()
    // The '/' is consumed by the shortcut, not typed into the box.
    expect((input as HTMLInputElement).value).toBe('')
  })

  it('debounces: a change issues a single search request after the delay', async () => {
    renderSearch()
    const input = screen.getByLabelText(en.search.label)
    // A single change; the debounce collapses it into one request after 250ms.
    fireEvent.change(input, { target: { value: 'ali' } })
    // Not fired immediately.
    expect(fetchMock).not.toHaveBeenCalled()

    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1))
    const url = fetchMock.mock.calls[0][0] as string
    expect(url).toContain('/search?q=ali')
  })

  it('renders grouped results after a query resolves', async () => {
    const user = userEvent.setup()
    renderSearch()
    await user.type(screen.getByLabelText(en.search.label), 'ali')
    expect(await screen.findByText('ali')).toBeInTheDocument()
    expect(screen.getByText(en.search.groupSubscribers)).toBeInTheDocument()
  })
})
