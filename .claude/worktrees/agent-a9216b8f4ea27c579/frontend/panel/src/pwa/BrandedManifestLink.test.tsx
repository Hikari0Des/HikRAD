import { render, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { BrandedManifestLink } from './BrandedManifestLink'

// v2 phase 11 (FR-92, contract C7) regression: this component already
// called the public branding endpoint correctly pre-phase — it was the
// endpoint itself that silently served defaults (see
// docs/ops/known-issues.md). This proves it now swaps the manifest/
// theme-color to REAL configured data once the endpoint is fixed.

function stubBrandingFetch() {
  vi.stubGlobal(
    'fetch',
    vi.fn(() =>
      Promise.resolve(
        new Response(
          JSON.stringify({
            name: 'Nur Net',
            logo_url: '/api/v1/branding/logo?v=abc123',
            theme_color: '#e11d48',
            background_color: '#111827',
          }),
          { status: 200 },
        ),
      ),
    ),
  )
}

beforeEach(() => {
  stubBrandingFetch()
  // jsdom does not implement URL.createObjectURL/revokeObjectURL.
  URL.createObjectURL = URL.createObjectURL ?? (() => 'blob:mock')
  URL.revokeObjectURL = URL.revokeObjectURL ?? (() => {})
  document.head
    .querySelectorAll('link[rel="manifest"], meta[name="theme-color"]')
    .forEach((el) => el.remove())
})

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('panel BrandedManifestLink (FR-92, contract C7)', () => {
  it('swaps the manifest link and theme-color meta once configured branding resolves', async () => {
    render(<BrandedManifestLink />)

    await waitFor(() => {
      const link = document.querySelector<HTMLLinkElement>('link[rel="manifest"]')
      expect(link?.href).toMatch(/^blob:/)
    })
    const meta = document.querySelector<HTMLMetaElement>('meta[name="theme-color"]')
    expect(meta?.content).toBe('#e11d48')
  })
})
