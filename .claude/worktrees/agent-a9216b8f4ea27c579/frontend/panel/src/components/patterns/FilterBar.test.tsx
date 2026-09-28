import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { I18nProvider } from '@hikrad/shared'
import en from '@hikrad/shared/locales/en/panel.json'

import { FilterBar, type FilterField } from './FilterBar'

const fields: FilterField[] = [
  {
    key: 'status',
    label: 'Status',
    options: [
      { value: 'active', label: 'Active' },
      { value: 'expired', label: 'Expired' },
    ],
  },
]

function renderBar(
  values: Record<string, string>,
  onChange = vi.fn(),
  onSearchChange = vi.fn(),
  search = '',
) {
  render(
    <I18nProvider>
      <FilterBar
        search={search}
        onSearchChange={onSearchChange}
        fields={fields}
        values={values}
        onChange={onChange}
      />
    </I18nProvider>,
  )
  return { onChange, onSearchChange }
}

beforeEach(() => window.localStorage.clear())

describe('FilterBar (FR-98.1)', () => {
  it('renders an active filter as a removable chip and clears it', async () => {
    const user = userEvent.setup()
    const { onChange } = renderBar({ status: 'active' })
    // Chip shows "Status: Active" (field label + option label, not the raw value).
    expect(screen.getByText('Status: Active')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: en.ui.remove }))
    expect(onChange).toHaveBeenCalledWith({})
  })

  it('typing in the search box calls onSearchChange', async () => {
    const user = userEvent.setup()
    const { onSearchChange } = renderBar({})
    await user.type(screen.getByLabelText(en.ui.search), 'ali')
    expect(onSearchChange).toHaveBeenCalled()
  })

  it('offers the add-filter menu only while a field is unapplied', () => {
    // status already applied → no "Add filter" menu (it was the only field).
    renderBar({ status: 'active' })
    expect(screen.queryByLabelText(en.filter.add)).not.toBeInTheDocument()
  })

  it('shows clear-all when a filter is applied', () => {
    renderBar({ status: 'active' })
    expect(screen.getByText(en.users.clearFilters)).toBeInTheDocument()
  })
})
