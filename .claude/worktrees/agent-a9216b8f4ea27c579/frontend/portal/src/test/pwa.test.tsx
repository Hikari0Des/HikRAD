import { act, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { I18nProvider } from '@hikrad/shared'

import en from '@hikrad/shared/locales/en/portal.json'
import { OfflineBanner } from '../pwa/OfflineBanner'
import * as swModule from '../pwa/registerServiceWorker'
import { UpdateToast } from '../pwa/UpdateToast'

function setOnline(value: boolean) {
  Object.defineProperty(window.navigator, 'onLine', { configurable: true, value })
  window.dispatchEvent(new Event(value ? 'online' : 'offline'))
}

afterEach(() => {
  setOnline(true)
})

describe('offline screen trigger (FR-54.2)', () => {
  it('shows the honest offline banner only while navigator.onLine is false', () => {
    render(
      <I18nProvider>
        <OfflineBanner />
      </I18nProvider>,
    )
    expect(screen.queryByText(en.portal.pwa.offline)).not.toBeInTheDocument()

    act(() => setOnline(false))
    expect(screen.getByText(en.portal.pwa.offline)).toBeInTheDocument()

    act(() => setOnline(true))
    expect(screen.queryByText(en.portal.pwa.offline)).not.toBeInTheDocument()
  })
})

describe('SW update prompt (FR-54.3)', () => {
  const applySpy = vi.spyOn(swModule, 'applyServiceWorkerUpdate').mockImplementation(() => {})

  beforeEach(() => {
    applySpy.mockClear()
  })

  it('appears when a waiting service worker is detected and applies the update on tap', async () => {
    const user = userEvent.setup()
    render(
      <I18nProvider>
        <UpdateToast />
      </I18nProvider>,
    )

    expect(screen.queryByText(en.portal.pwa.updateAvailable)).not.toBeInTheDocument()

    act(() => window.dispatchEvent(new Event(swModule.SW_UPDATE_EVENT)))
    expect(screen.getByText(en.portal.pwa.updateAvailable)).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: en.portal.pwa.updateAction }))
    expect(applySpy).toHaveBeenCalledOnce()
    expect(screen.queryByText(en.portal.pwa.updateAvailable)).not.toBeInTheDocument()
  })
})
