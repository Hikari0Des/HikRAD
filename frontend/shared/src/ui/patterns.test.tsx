import { render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import type { ReactNode } from 'react'

import { I18nProvider } from '../i18n/I18nProvider'
import { Card, SectionCard, StatCard } from './Card'
import { DataTable, type Column } from './DataTable'

function withI18n(ui: ReactNode) {
  return render(<I18nProvider>{ui}</I18nProvider>)
}

describe('Card / StatCard / SectionCard (FR-97.2)', () => {
  it('renders a StatCard label + value and a SectionCard title', () => {
    withI18n(
      <>
        <StatCard label="Active" value={128} hint="7 days" />
        <SectionCard title="Summary">
          <Card>body</Card>
        </SectionCard>
      </>,
    )
    expect(screen.getByText('Active')).toBeInTheDocument()
    expect(screen.getByText('128')).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: 'Summary' })).toBeInTheDocument()
    expect(screen.getByText('body')).toBeInTheDocument()
  })
})

interface Row {
  id: string
  name: string
}
const cols: Column<Row>[] = [
  { key: 'name', header: 'Name', cell: (r) => r.name },
  { key: 'id', header: 'ID', cell: (r) => r.id, align: 'end' },
]

describe('DataTable (FR-97.2)', () => {
  it('renders rows under the column headers', () => {
    withI18n(<DataTable columns={cols} rows={[{ id: '1', name: 'Ali' }]} getRowKey={(r) => r.id} />)
    expect(screen.getByText('Name')).toBeInTheDocument()
    expect(screen.getByText('Ali')).toBeInTheDocument()
  })

  it('shows the localized empty state when there are no rows', () => {
    withI18n(<DataTable columns={cols} rows={[]} getRowKey={(r) => r.id} />)
    expect(screen.getByText('Nothing here yet')).toBeInTheDocument()
  })

  it('shows the error state with a retry when error is set', () => {
    const onRetry = vi.fn()
    withI18n(<DataTable columns={cols} rows={[]} getRowKey={(r) => r.id} error onRetry={onRetry} />)
    expect(screen.getByRole('alert')).toBeInTheDocument()
    screen.getByRole('button').click()
    expect(onRetry).toHaveBeenCalled()
  })

  it('shows loading when loading and no rows yet', () => {
    withI18n(<DataTable columns={cols} rows={[]} getRowKey={(r) => r.id} loading />)
    expect(screen.getByRole('status')).toBeInTheDocument()
  })
})
