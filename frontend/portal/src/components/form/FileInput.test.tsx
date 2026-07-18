import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { FileInput } from './FileInput'

describe('FileInput (FR-94, new control)', () => {
  it('calls onFilesSelected when a file is chosen via the native input', () => {
    const onFilesSelected = vi.fn()
    render(<FileInput label="Attach proof" onFilesSelected={onFilesSelected} />)
    const file = new File(['x'], 'proof.png', { type: 'image/png' })
    const input = screen.getByLabelText('Attach proof', { selector: 'input' })
    fireEvent.change(input, { target: { files: [file] } })
    expect(onFilesSelected).toHaveBeenCalledTimes(1)
    const files = onFilesSelected.mock.calls[0][0] as FileList
    expect(files[0].name).toBe('proof.png')
  })

  it('the native input is visually hidden but present (keeps keyboard/SR behavior)', () => {
    render(<FileInput label="Attach proof" onFilesSelected={() => {}} />)
    const input = screen.getByLabelText('Attach proof', { selector: 'input' })
    expect(input).toHaveClass('sr-only')
  })
})
