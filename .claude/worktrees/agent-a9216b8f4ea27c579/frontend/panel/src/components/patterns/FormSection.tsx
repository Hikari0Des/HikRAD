import { useId, useState, type ReactNode } from 'react'

import { useT } from '@hikrad/shared'

import { Field } from '../form'

/**
 * v3-1 FR-97.2 form-layout primitives.
 *
 * FormSection groups fields by task (identity → service → billing → advanced,
 * FR-98.2). An `advanced` section is collapsed by default with an accessible
 * toggle, so rarely-used fields don't crowd the common path.
 */
export function FormSection({
  title,
  advanced = false,
  defaultOpen,
  children,
}: {
  title: string
  /** Marks the section as the collapsed-by-default "Advanced" group. */
  advanced?: boolean
  defaultOpen?: boolean
  children: ReactNode
}) {
  const [open, setOpen] = useState(defaultOpen ?? !advanced)
  const bodyId = useId()

  if (!advanced) {
    return (
      <fieldset className="min-w-0">
        <legend className="mb-2 text-xs font-semibold uppercase tracking-wide text-ink-muted">
          {title}
        </legend>
        <div className="grid gap-3 sm:grid-cols-2">{children}</div>
      </fieldset>
    )
  }

  return (
    <fieldset className="min-w-0">
      <button
        type="button"
        className="mb-2 flex w-full items-center justify-between text-xs font-semibold uppercase tracking-wide text-ink-muted"
        aria-expanded={open}
        aria-controls={bodyId}
        onClick={() => setOpen((o) => !o)}
      >
        <span>{title}</span>
        <Chevron open={open} />
      </button>
      {open ? (
        <div id={bodyId} className="grid gap-3 sm:grid-cols-2">
          {children}
        </div>
      ) : null}
    </fieldset>
  )
}

/**
 * FormField wraps `Field` and associates its visible label with the control
 * **by construction** (FR-97.2 / AC-97a): it generates an id and hands it to
 * the render-prop child, so no caller has to remember `htmlFor`. This closes
 * the long-open association gap for every new form the redesign touches.
 */
export function FormField({
  label,
  error,
  hint,
  children,
}: {
  label: string
  error?: string
  hint?: string
  children: (id: string) => ReactNode
}) {
  const id = useId()
  return (
    <Field label={label} error={error} hint={hint} htmlFor={id}>
      {children(id)}
    </Field>
  )
}

function Chevron({ open }: { open: boolean }) {
  const t = useT()
  return (
    <svg
      viewBox="0 0 12 12"
      width="10"
      height="10"
      fill="none"
      aria-label={open ? t('ui.collapse') : t('ui.expand')}
      role="img"
      style={{ transform: open ? 'rotate(180deg)' : undefined, transition: 'transform 150ms' }}
    >
      <path
        d="M2.5 4.5 6 8l3.5-3.5"
        stroke="currentColor"
        strokeWidth="1.4"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </svg>
  )
}
