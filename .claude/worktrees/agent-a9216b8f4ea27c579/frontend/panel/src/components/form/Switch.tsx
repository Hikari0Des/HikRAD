import * as RadixSwitch from '@radix-ui/react-switch'
import { useId, type ReactNode } from 'react'

import { FOCUS_RING } from './shared'

/**
 * Toggle switch (FR-94, new control — no pre-modernization equivalent).
 * The thumb slides via `margin-inline-start`, not `transform: translateX` —
 * a logical property flips automatically under `dir="rtl"` (NFR-6.2), where
 * a physical-axis transform would need a separate `rtl:` override. Margin is
 * animatable, so this keeps a smooth slide while staying RTL-correct by
 * construction; the transition itself is reduced-motion-guarded (C8).
 */
export function Switch({
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
  onChange: (e: { target: { checked: boolean } }) => void
  disabled?: boolean
  className?: string
  'aria-label'?: string
}) {
  const id = useId()

  const control = (
    <RadixSwitch.Root
      id={id}
      checked={checked}
      disabled={disabled}
      aria-label={rest['aria-label']}
      onCheckedChange={(v) => onChange({ target: { checked: v } })}
      className={`relative inline-flex h-5 w-9 shrink-0 items-center rounded-full bg-surface-sunken p-0.5 data-[state=checked]:bg-brand disabled:opacity-60 ${FOCUS_RING} ${className ?? ''}`}
    >
      <RadixSwitch.Thumb className="ms-0 block h-4 w-4 rounded-full bg-surface-raised shadow transition-[margin-inline-start] duration-150 motion-reduce:transition-none data-[state=checked]:ms-[16px]" />
    </RadixSwitch.Root>
  )

  if (!label) return control

  return (
    <div className="flex items-center gap-2">
      {control}
      <label htmlFor={id} className="text-sm">
        <span className="font-medium">{label}</span>
        {description ? <span className="block text-xs text-ink-muted">{description}</span> : null}
      </label>
    </div>
  )
}
