import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { ChartContainer, Ltr } from './Ltr'

describe('Ltr bidi isolation', () => {
  it('renders a <bdi dir="ltr"> around machine values', () => {
    render(
      <p dir="rtl">
        {/* i18n-exempt — literal machine values under test */}
        <Ltr>AA:BB:CC:DD:EE:FF</Ltr>
      </p>,
    )
    const bdi = screen.getByText('AA:BB:CC:DD:EE:FF')
    expect(bdi.tagName.toLowerCase()).toBe('bdi')
    expect(bdi).toHaveAttribute('dir', 'ltr')
  })

  it('passes className through', () => {
    render(<Ltr className="font-mono">user01</Ltr>)
    expect(screen.getByText('user01')).toHaveClass('font-mono')
  })
})

describe('ChartContainer', () => {
  it('pins charts LTR inside RTL pages', () => {
    render(
      <div dir="rtl">
        <ChartContainer>
          <svg data-testid="chart" />
        </ChartContainer>
      </div>,
    )
    const container = screen.getByTestId('chart').parentElement
    expect(container).toHaveAttribute('dir', 'ltr')
  })
})
