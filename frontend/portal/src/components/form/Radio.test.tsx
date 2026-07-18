import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { RadioGroup, RadioOption } from './Radio'

describe('RadioGroup / RadioOption (FR-94, C1, C5 — replaces hand-rolled radio groups)', () => {
  it('renders no native <input type="radio"> element', () => {
    const { container } = render(
      <RadioGroup value="a" onValueChange={() => {}}>
        <RadioOption value="a" label="A" />
        <RadioOption value="b" label="B" />
      </RadioGroup>,
    )
    expect(container.querySelector('input[type="radio"]')).toBeNull()
    expect(screen.getAllByRole('radio')).toHaveLength(2)
  })

  it('marks the current value as checked and calls onValueChange when another option is picked', () => {
    const onValueChange = vi.fn()
    render(
      <RadioGroup value="a" onValueChange={onValueChange}>
        <RadioOption value="a" label="A" />
        <RadioOption value="b" label="B" />
      </RadioGroup>,
    )
    expect(screen.getByRole('radio', { name: 'A' })).toBeChecked()
    expect(screen.getByRole('radio', { name: 'B' })).not.toBeChecked()
    fireEvent.click(screen.getByRole('radio', { name: 'B' }))
    expect(onValueChange).toHaveBeenCalledWith('b')
  })

  it('supports a disabled option', () => {
    render(
      <RadioGroup value="a" onValueChange={() => {}}>
        <RadioOption value="a" label="A" />
        <RadioOption value="b" label="B" disabled />
      </RadioGroup>,
    )
    expect(screen.getByRole('radio', { name: 'B' })).toBeDisabled()
  })
})
