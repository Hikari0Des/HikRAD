import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { TextInput } from './TextInput'

describe('TextInput (FR-94, restyled native input — not CI-gated, C4)', () => {
  it('forwards native input attributes and onChange unchanged', () => {
    const onChange = vi.fn()
    render(<TextInput aria-label="Username" value="a" onChange={onChange} />)
    fireEvent.change(screen.getByLabelText('Username'), { target: { value: 'ab' } })
    expect(onChange).toHaveBeenCalled()
  })

  it('has a visible focus-ring class', () => {
    render(<TextInput aria-label="Username" value="" onChange={() => {}} />)
    expect(screen.getByLabelText('Username').className).toMatch(/focus-visible:ring-2/)
  })
})
