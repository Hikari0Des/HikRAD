import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { Select } from './Select'

describe('Select (FR-94, C1)', () => {
  it('renders no native <select> element', () => {
    const { container } = render(
      <Select value="a" onChange={() => {}}>
        <option value="a">A</option>
      </Select>,
    )
    expect(container.querySelector('select')).toBeNull()
    expect(screen.getByRole('combobox')).toBeInTheDocument()
  })

  it('shows the current value label on the trigger', () => {
    render(
      <Select value="b" onChange={() => {}}>
        <option value="a">Alpha</option>
        <option value="b">Beta</option>
      </Select>,
    )
    expect(screen.getByRole('combobox')).toHaveTextContent('Beta')
  })

  it('calls onChange with the selected option value when a real option is picked', async () => {
    const onChange = vi.fn()
    render(
      <Select value="a" onChange={onChange}>
        <option value="a">Alpha</option>
        <option value="b">Beta</option>
      </Select>,
    )
    fireEvent.click(screen.getByRole('combobox'))
    fireEvent.click(await screen.findByRole('option', { name: 'Beta' }))
    expect(onChange).toHaveBeenCalledWith({ target: { value: 'b' } })
  })

  it('round-trips an empty-string option value (the "none" placeholder pattern used across the panel)', async () => {
    const onChange = vi.fn()
    render(
      <Select value="" onChange={onChange}>
        <option value="">None</option>
        <option value="x">X</option>
      </Select>,
    )
    // The trigger shows the empty option's label without throwing (Radix
    // forbids an Item value="" internally — this asserts the sentinel
    // mapping in Select.tsx actually works end to end).
    expect(screen.getByRole('combobox')).toHaveTextContent('None')
    fireEvent.click(screen.getByRole('combobox'))
    fireEvent.click(await screen.findByRole('option', { name: 'X' }))
    expect(onChange).toHaveBeenCalledWith({ target: { value: 'x' } })
  })

  it('respects a disabled option (VouchersPage-style disabled placeholder)', async () => {
    const onChange = vi.fn()
    render(
      <Select value="x" onChange={onChange}>
        <option value="" disabled>
          Choose one
        </option>
        <option value="x">X</option>
      </Select>,
    )
    fireEvent.click(screen.getByRole('combobox'))
    const disabledOption = await screen.findByRole('option', { name: 'Choose one' })
    expect(disabledOption).toHaveAttribute('data-disabled')
  })
})
