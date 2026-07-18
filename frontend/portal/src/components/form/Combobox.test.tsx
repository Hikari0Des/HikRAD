import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { Combobox, type ComboboxOption } from './Combobox'

const options: ComboboxOption[] = [
  { value: 'nas1', label: 'Karrada Tower' },
  { value: 'nas1:svc1', label: 'Karrada — Lobby (hotspot)', indent: true },
  { value: 'nas2', label: 'Mansour Tower' },
]

describe('Combobox (FR-94, C6 — replaces NasScopePicker-style hand-rolled popovers)', () => {
  it('opens on trigger click and lists every option as role="option"', async () => {
    render(
      <Combobox
        options={options}
        selected={[]}
        onChange={() => {}}
        triggerLabel="Add"
        noOptionsLabel="none"
        noMatchLabel="no match"
      />,
    )
    fireEvent.click(screen.getByRole('button', { name: 'Add' }))
    expect(await screen.findAllByRole('option')).toHaveLength(3)
  })

  it('toggles a value into the selection on click', async () => {
    const onChange = vi.fn()
    render(
      <Combobox
        options={options}
        selected={[]}
        onChange={onChange}
        triggerLabel="Add"
        noOptionsLabel="none"
        noMatchLabel="no match"
      />,
    )
    fireEvent.click(screen.getByRole('button', { name: 'Add' }))
    fireEvent.click(await screen.findByRole('option', { name: 'Mansour Tower' }))
    expect(onChange).toHaveBeenCalledWith(['nas2'])
  })

  it('removes an already-selected value on a second click', async () => {
    const onChange = vi.fn()
    render(
      <Combobox
        options={options}
        selected={['nas2']}
        onChange={onChange}
        triggerLabel="Add"
        noOptionsLabel="none"
        noMatchLabel="no match"
      />,
    )
    fireEvent.click(screen.getByRole('button', { name: 'Add' }))
    fireEvent.click(await screen.findByRole('option', { name: 'Mansour Tower' }))
    expect(onChange).toHaveBeenCalledWith([])
  })

  it('filters the list as the search box is typed into', async () => {
    render(
      <Combobox
        options={options}
        selected={[]}
        onChange={() => {}}
        triggerLabel="Add"
        noOptionsLabel="none"
        noMatchLabel="no match"
      />,
    )
    fireEvent.click(screen.getByRole('button', { name: 'Add' }))
    const input = await screen.findByRole('textbox')
    fireEvent.change(input, { target: { value: 'Mansour' } })
    expect(screen.getAllByRole('option')).toHaveLength(1)
    expect(screen.getByRole('option', { name: 'Mansour Tower' })).toBeInTheDocument()
  })

  it('shows the empty-options message when there are no options at all', async () => {
    render(
      <Combobox
        options={[]}
        selected={[]}
        onChange={() => {}}
        triggerLabel="Add"
        noOptionsLabel="Nothing to pick from"
        noMatchLabel="no match"
      />,
    )
    fireEvent.click(screen.getByRole('button', { name: 'Add' }))
    expect(await screen.findByText('Nothing to pick from')).toBeInTheDocument()
  })
})
