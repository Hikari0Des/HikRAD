import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { Textarea } from './Textarea'

describe('Textarea (FR-94, restyled native textarea — not CI-gated, C4)', () => {
  it('forwards native textarea attributes and onChange unchanged', () => {
    const onChange = vi.fn()
    render(<Textarea aria-label="Notes" value="a" onChange={onChange} />)
    fireEvent.change(screen.getByLabelText('Notes'), { target: { value: 'ab' } })
    expect(onChange).toHaveBeenCalled()
  })
})
