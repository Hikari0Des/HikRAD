import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { Checkbox } from './Checkbox'

describe('Checkbox (FR-94, C1)', () => {
  it('renders no native <input type="checkbox"> element', () => {
    const { container } = render(<Checkbox label="Opt in" checked={false} onChange={() => {}} />)
    expect(container.querySelector('input[type="checkbox"]')).toBeNull()
    expect(screen.getByRole('checkbox')).toBeInTheDocument()
  })

  it('calls onChange with { target: { checked } } on click (native-event-shaped, backward compatible)', () => {
    const onChange = vi.fn()
    render(<Checkbox label="Opt in" checked={false} onChange={onChange} />)
    fireEvent.click(screen.getByRole('checkbox'))
    expect(onChange).toHaveBeenCalledWith({ target: { checked: true } })
  })

  it('renders with no label for standalone table-cell usage, using aria-label instead', () => {
    render(<Checkbox checked onChange={() => {}} aria-label="Select row" />)
    expect(screen.getByRole('checkbox', { name: 'Select row' })).toBeChecked()
  })

  it('does not fire onChange when disabled', () => {
    const onChange = vi.fn()
    render(<Checkbox label="Opt in" checked={false} onChange={onChange} disabled />)
    fireEvent.click(screen.getByRole('checkbox'))
    expect(onChange).not.toHaveBeenCalled()
  })
})
