import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import type { ReactNode } from 'react'

import { I18nProvider } from '../i18n/I18nProvider'
import { IQDAmount } from './IQDAmount'
import { QuotaBar } from './QuotaBar'
import { StatusBadge } from './StatusBadge'
import { EmptyState, ErrorState, LoadingState } from './states'

function withI18n(ui: ReactNode, locale?: 'en' | 'ar' | 'ku') {
  if (locale) window.localStorage.setItem('hikrad.locale', locale)
  return render(<I18nProvider>{ui}</I18nProvider>)
}

describe('StatusBadge', () => {
  it('localizes the status label', () => {
    withI18n(<StatusBadge status="active" />, 'ar')
    expect(screen.getByText('نشط')).toBeInTheDocument()
  })
})

describe('QuotaBar', () => {
  it('exposes progressbar semantics and a localized percent', () => {
    withI18n(<QuotaBar used={42} total={100} />)
    const bar = screen.getByRole('progressbar')
    expect(bar).toHaveAttribute('aria-valuenow', '42')
    expect(bar).toHaveAttribute('aria-valuemax', '100')
    expect(screen.getByText('42%')).toBeInTheDocument()
  })

  it('clamps overflow and switches to the warn color', () => {
    withI18n(<QuotaBar used={150} total={100} />)
    expect(screen.getByText('100%')).toBeInTheDocument()
    expect(screen.getByRole('progressbar').parentElement).toHaveClass('hk-quota--warn')
  })
})

describe('states', () => {
  it('renders localized loading/empty/error defaults', () => {
    withI18n(
      <>
        <LoadingState />
        <EmptyState />
        <ErrorState onRetry={() => {}} />
      </>,
    )
    expect(screen.getByText('Loading…')).toBeInTheDocument()
    expect(screen.getByText('Nothing here yet')).toBeInTheDocument()
    expect(screen.getByRole('alert')).toHaveTextContent('Something went wrong')
    expect(screen.getByRole('button')).toHaveTextContent('Try again')
  })
})

describe('IQDAmount', () => {
  it('renders a bidi-isolated IQD amount', () => {
    withI18n(<IQDAmount amount={25000} />)
    const el = screen.getByText(/25,000/)
    expect(el.tagName.toLowerCase()).toBe('bdi')
    expect(el.textContent).toContain('IQD')
  })

  it('renders a non-IQD currency via the currency prop (v2 phase 4)', () => {
    withI18n(<IQDAmount amount={150.5} currency="USD" />)
    const el = screen.getByText(/150\.50/)
    expect(el.tagName.toLowerCase()).toBe('bdi')
    expect(el.textContent).toContain('$')
  })
})
