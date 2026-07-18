import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { Switch } from './Switch'

describe('Switch (FR-94, new control)', () => {
  it('renders as a switch role, not a checkbox', () => {
    render(<Switch label="Enabled" checked={false} onChange={() => {}} />)
    expect(screen.getByRole('switch')).toBeInTheDocument()
  })

  it('calls onChange with { target: { checked } } on click', () => {
    const onChange = vi.fn()
    render(<Switch label="Enabled" checked={false} onChange={onChange} />)
    fireEvent.click(screen.getByRole('switch'))
    expect(onChange).toHaveBeenCalledWith({ target: { checked: true } })
  })

  it('renders standalone with no label', () => {
    render(<Switch checked aria-label="Toggle" onChange={() => {}} />)
    expect(screen.getByRole('switch')).toBeChecked()
  })
})
