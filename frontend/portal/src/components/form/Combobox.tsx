import * as RadixPopover from '@radix-ui/react-popover'
import { useMemo, useState, type ReactNode } from 'react'

import { Checkbox } from './Checkbox'
import { CONTROL, FOCUS_RING, POPOVER_CONTENT } from './shared'

export interface ComboboxOption {
  value: string
  label: string
  /** Visually indented under a parent row (e.g. a NAS's service instances). */
  indent?: boolean
}

/**
 * Searchable multi-select popover (FR-94's "combobox where lists are long"
 * case — NAS/profile pickers). Built on `@radix-ui/react-popover` + the new
 * `Checkbox` (contract C6), replacing `NasScopePicker`'s hand-rolled
 * mousedown/Escape-listener popover with Radix's real focus trap and
 * dismiss handling, while keeping the same visible shape: a search-filtered
 * `role="listbox"` of checkable rows, indented service rows under their NAS.
 * Chip rendering / empty-state text stay the caller's own concern (they are
 * product-specific, not generic combobox behavior) — this component owns
 * only the trigger + popover + filtered list + selection.
 */
export function Combobox({
  options,
  selected,
  onChange,
  triggerLabel,
  searchPlaceholder,
  noOptionsLabel,
  noMatchLabel,
  disabled,
  className,
}: {
  options: ComboboxOption[]
  selected: string[]
  onChange: (next: string[]) => void
  triggerLabel: ReactNode
  searchPlaceholder?: string
  noOptionsLabel: string
  noMatchLabel: string
  disabled?: boolean
  className?: string
}) {
  const [open, setOpen] = useState(false)
  const [query, setQuery] = useState('')

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase()
    if (!q) return options
    return options.filter((o) => o.label.toLowerCase().includes(q))
  }, [options, query])

  const toggle = (value: string) => {
    onChange(selected.includes(value) ? selected.filter((v) => v !== value) : [...selected, value])
  }

  return (
    <RadixPopover.Root
      open={open}
      onOpenChange={(next) => {
        setOpen(next)
        if (!next) setQuery('')
      }}
    >
      <RadixPopover.Trigger asChild>
        <button
          type="button"
          disabled={disabled}
          aria-expanded={open}
          aria-haspopup="listbox"
          className={`${CONTROL} text-start ${FOCUS_RING} ${className ?? ''}`}
        >
          {triggerLabel}
        </button>
      </RadixPopover.Trigger>
      <RadixPopover.Portal>
        <RadixPopover.Content
          align="start"
          sideOffset={4}
          className={`w-[var(--radix-popover-trigger-width)] p-0 ${POPOVER_CONTENT}`}
        >
          <div className="border-b border-surface-sunken p-2">
            <input
              type="text"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder={searchPlaceholder}
              autoFocus
              className="w-full rounded border border-surface-sunken bg-surface px-2 py-1 text-sm focus:outline-none"
            />
          </div>
          <div role="listbox" aria-multiselectable className="max-h-64 overflow-y-auto py-1">
            {options.length === 0 ? (
              <p className="p-3 text-sm text-ink-muted">{noOptionsLabel}</p>
            ) : filtered.length === 0 ? (
              <p className="p-3 text-sm text-ink-muted">{noMatchLabel}</p>
            ) : (
              filtered.map((opt) => {
                const checked = selected.includes(opt.value)
                return (
                  <label
                    key={opt.value}
                    role="option"
                    aria-selected={checked}
                    className={`flex cursor-pointer items-center gap-2 px-3 py-1.5 text-sm hover:bg-surface-sunken/50 ${
                      opt.indent ? 'ps-8' : 'font-medium'
                    }`}
                  >
                    {/* No separate aria-label: the wrapping <label role="option"> already
                        supplies the accessible name from its visible text (opt.label below) —
                        adding one here would double it up in the accname computation. */}
                    <Checkbox checked={checked} onChange={() => toggle(opt.value)} />
                    {opt.label}
                  </label>
                )
              })
            )}
          </div>
        </RadixPopover.Content>
      </RadixPopover.Portal>
    </RadixPopover.Root>
  )
}
