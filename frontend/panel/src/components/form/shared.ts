/**
 * Shared Tailwind class fragments for the control set (C2/C8). Kept as a
 * plain string constant, not CSS classes in @hikrad/shared, since every
 * consumer here is already Tailwind-based (same posture as Modal.tsx) — a
 * dependency-free string is simpler than a parallel CSS file for utility
 * composition alone. Genuinely shared *non-Tailwind* pieces (if any) still
 * belong in @hikrad/shared/src/ui per contract C2.
 */
export const CONTROL =
  'w-full rounded-md border border-surface-sunken bg-surface px-3 py-2 text-sm ' +
  'focus:outline-none focus-visible:ring-2 focus-visible:ring-brand focus-visible:ring-offset-1 ' +
  'focus-visible:ring-offset-surface disabled:opacity-60'

/** Focus ring for non-text controls (checkbox/radio/switch/select trigger/combobox trigger). */
export const FOCUS_RING =
  'focus:outline-none focus-visible:ring-2 focus-visible:ring-brand focus-visible:ring-offset-1 ' +
  'focus-visible:ring-offset-surface'

/**
 * Popover/select content chrome, shared across Select/Combobox (both use
 * Radix Portal + this shell). No animation library dependency — a plain
 * opacity/scale transition driven by Radix's own data-state attribute
 * (Tailwind core arbitrary-attribute variants, no plugin needed), guarded by
 * motion-reduce: per C8's reduced-motion rule.
 */
export const POPOVER_CONTENT =
  'z-50 max-h-72 overflow-y-auto rounded-md border border-surface-sunken bg-surface-raised py-1 shadow-lg ' +
  'transition-[opacity,transform] duration-150 motion-reduce:transition-none ' +
  'data-[state=closed]:opacity-0 data-[state=closed]:scale-95 ' +
  'data-[state=open]:opacity-100 data-[state=open]:scale-100'

/** A single selectable row inside Select/Combobox content. */
export const POPOVER_ITEM =
  'relative flex cursor-pointer select-none items-center gap-2 px-3 py-1.5 text-sm text-start outline-none ' +
  'data-[highlighted]:bg-brand-soft data-[disabled]:pointer-events-none data-[disabled]:opacity-50'
