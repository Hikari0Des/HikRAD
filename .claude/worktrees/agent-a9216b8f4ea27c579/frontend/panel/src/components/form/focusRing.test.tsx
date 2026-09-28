import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { Checkbox } from './Checkbox'
import { Combobox } from './Combobox'
import { RadioGroup, RadioOption } from './Radio'
import { Select } from './Select'
import { Switch } from './Switch'
import { TextInput } from './TextInput'

/**
 * C8 (polish sweep): every interactive control must have a visible focus
 * ring — `form.tsx`'s pre-modernization `CONTROL` constant set
 * `focus:outline-none` with no `focus:ring-*` replacement, a real
 * accessibility gap this phase closes. One instance per control is enough
 * to prove the class is actually applied, not aspirational.
 */
describe('Focus rings present on every new control (C8)', () => {
  it('TextInput', () => {
    render(<TextInput aria-label="x" value="" onChange={() => {}} />)
    expect(screen.getByLabelText('x').className).toMatch(/focus-visible:ring-2/)
  })

  it('Select trigger', () => {
    render(
      <Select value="a" onChange={() => {}}>
        <option value="a">A</option>
      </Select>,
    )
    expect(screen.getByRole('combobox').className).toMatch(/focus-visible:ring-2/)
  })

  it('Checkbox', () => {
    render(<Checkbox checked={false} onChange={() => {}} aria-label="x" />)
    expect(screen.getByRole('checkbox').className).toMatch(/focus-visible:ring-2/)
  })

  it('RadioOption', () => {
    render(
      <RadioGroup value="a" onValueChange={() => {}}>
        <RadioOption value="a" label="A" />
      </RadioGroup>,
    )
    expect(screen.getByRole('radio').className).toMatch(/focus-visible:ring-2/)
  })

  it('Switch', () => {
    render(<Switch checked={false} onChange={() => {}} aria-label="x" />)
    expect(screen.getByRole('switch').className).toMatch(/focus-visible:ring-2/)
  })

  it('Combobox trigger', () => {
    render(
      <Combobox
        options={[]}
        selected={[]}
        onChange={() => {}}
        triggerLabel="Add"
        noOptionsLabel="none"
        noMatchLabel="no match"
      />,
    )
    expect(screen.getByRole('button', { name: 'Add' }).className).toMatch(/focus-visible:ring-2/)
  })
})
