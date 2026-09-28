import { render, screen } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { I18nProvider } from '@hikrad/shared'

import { PrintHeader } from './PrintHeader'

beforeEach(() => {
  // useBranding() fetches /api/v1/branding directly; keep it a no-op here.
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(null, { status: 404 })))
})

describe('PrintHeader (task 1: print view)', () => {
  it('renders the report title and a generated-at timestamp, hidden outside print', () => {
    render(
      <I18nProvider>
        <PrintHeader reportTitle="Revenue report" />
      </I18nProvider>,
    )
    expect(screen.getByText('Revenue report')).toBeInTheDocument()
    // Only visible in print (screen rendering keeps it out of the way).
    const header = screen.getByTestId('print-header')
    expect(header.className).toContain('print:block')
    expect(header.className).toContain('hidden')
  })
})
