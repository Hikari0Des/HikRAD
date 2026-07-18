import * as RadixRadioGroup from '@radix-ui/react-radio-group'
import { useId, type ReactNode } from 'react'

import { FOCUS_RING } from './shared'

/**
 * Radio group (FR-94) — replaces the hand-rolled `<input type="radio">`
 * groups this codebase had (`ServiceTypeRadio` in SubscriberFormModal.tsx,
 * `ResolutionChoice` in NasAutoSetupModal.tsx, C5). Grouping real Radix
 * items under one `RadioGroup.Root` (they were previously ungrouped/
 * unnamed inputs relying only on app state for mutual exclusivity) also
 * gains real roving-tabindex keyboard nav for free — an improvement, not a
 * behavior change to what the operator can select.
 */
export function RadioGroup({
  value,
  onValueChange,
  name,
  className,
  children,
}: {
  value: string
  onValueChange: (v: string) => void
  name?: string
  className?: string
  children: ReactNode
}) {
  return (
    <RadixRadioGroup.Root
      value={value}
      onValueChange={onValueChange}
      name={name}
      className={`flex flex-wrap gap-4 ${className ?? ''}`}
    >
      {children}
    </RadixRadioGroup.Root>
  )
}

export function RadioOption({
  value,
  label,
  disabled,
}: {
  value: string
  label: ReactNode
  disabled?: boolean
}) {
  const id = useId()
  return (
    <label htmlFor={id} className="flex items-center gap-2 text-sm">
      <RadixRadioGroup.Item
        id={id}
        value={value}
        disabled={disabled}
        className={`flex h-4 w-4 shrink-0 items-center justify-center rounded-full border border-surface-sunken bg-surface data-[state=checked]:border-brand disabled:opacity-60 ${FOCUS_RING}`}
      >
        <RadixRadioGroup.Indicator className="h-2 w-2 rounded-full bg-brand" />
      </RadixRadioGroup.Item>
      <span>{label}</span>
    </label>
  )
}
