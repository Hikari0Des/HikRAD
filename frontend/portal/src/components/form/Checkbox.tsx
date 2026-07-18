import * as RadixCheckbox from '@radix-ui/react-checkbox'
import { useId, type ReactNode } from 'react'

import { FOCUS_RING } from './shared'

export interface CheckboxChangeEvent {
  target: { checked: boolean }
}

/**
 * Radix-backed checkbox with a custom tick — replaces the bare
 * `<input type="checkbox">` the pre-modernization control set rendered
 * (contract C1/FR-94). `onChange` keeps the same `{ target: { checked } }`
 * shape the native-input version had (rather than Radix's raw boolean
 * `onCheckedChange`) so every existing call site
 * (`onChange={(e) => set('field', e.target.checked)}`) needs zero changes —
 * only files with a *bare* native checkbox (never this component) need
 * editing (C5).
 *
 * `label` is optional: a standalone checkbox in a table cell (no adjacent
 * visible text) passes `aria-label` instead — same pattern the native
 * version's callers already used.
 */
export function Checkbox({
  label,
  description,
  checked,
  onChange,
  disabled,
  className,
  ...rest
}: {
  label?: ReactNode
  description?: ReactNode
  checked: boolean
  onChange: (e: CheckboxChangeEvent) => void
  disabled?: boolean
  className?: string
  id?: string
  name?: string
  'aria-label'?: string
}) {
  const generatedId = useId()
  const id = rest.id ?? generatedId

  const control = (
    <RadixCheckbox.Root
      id={id}
      name={rest.name}
      checked={checked}
      disabled={disabled}
      aria-label={rest['aria-label']}
      onCheckedChange={(state) => onChange({ target: { checked: state === true } })}
      className={`${label ? 'mt-0.5' : ''} inline-flex h-4 w-4 shrink-0 items-center justify-center rounded border border-surface-sunken bg-surface data-[state=checked]:border-brand data-[state=checked]:bg-brand disabled:opacity-60 ${FOCUS_RING} ${className ?? ''}`}
    >
      <RadixCheckbox.Indicator className="text-ink-inverse">
        <CheckIcon />
      </RadixCheckbox.Indicator>
    </RadixCheckbox.Root>
  )

  if (!label) return control

  return (
    <div className="flex items-start gap-2">
      {control}
      <label htmlFor={id} className="text-sm">
        <span className="font-medium">{label}</span>
        {description ? <span className="block text-xs text-ink-muted">{description}</span> : null}
      </label>
    </div>
  )
}

function CheckIcon() {
  return (
    <svg viewBox="0 0 12 12" width="10" height="10" fill="none" aria-hidden="true">
      <path
        d="M2 6.2 4.8 9 10 3"
        stroke="currentColor"
        strokeWidth="1.8"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </svg>
  )
}
