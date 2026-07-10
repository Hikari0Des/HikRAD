import {
  type InputHTMLAttributes,
  type ReactNode,
  type SelectHTMLAttributes,
  type TextareaHTMLAttributes,
  useId,
} from 'react'

const CONTROL =
  'w-full rounded-md border border-surface-sunken bg-surface px-3 py-2 text-sm focus:border-brand focus:outline-none disabled:opacity-60'

/** Labelled field wrapper with an optional inline validation error. */
export function Field({
  label,
  error,
  hint,
  children,
  htmlFor,
}: {
  label: string
  error?: string
  hint?: string
  children: ReactNode
  htmlFor?: string
}) {
  return (
    <div>
      <label htmlFor={htmlFor} className="mb-1 block text-sm font-medium">
        {label}
      </label>
      {children}
      {hint && !error ? <p className="mt-1 text-xs text-ink-muted">{hint}</p> : null}
      {error ? (
        <p role="alert" className="mt-1 text-xs text-danger">
          {error}
        </p>
      ) : null}
    </div>
  )
}

export function TextInput(props: InputHTMLAttributes<HTMLInputElement>) {
  return <input {...props} className={`${CONTROL} ${props.className ?? ''}`} />
}

export function Textarea(props: TextareaHTMLAttributes<HTMLTextAreaElement>) {
  return <textarea {...props} className={`${CONTROL} ${props.className ?? ''}`} />
}

export function Select(props: SelectHTMLAttributes<HTMLSelectElement>) {
  return <select {...props} className={`${CONTROL} ${props.className ?? ''}`} />
}

/** Checkbox with an inline label (used for toggles like allow_hotspot). */
export function Checkbox({
  label,
  description,
  ...props
}: InputHTMLAttributes<HTMLInputElement> & { label: string; description?: string }) {
  const id = useId()
  return (
    <div className="flex items-start gap-2">
      <input id={id} type="checkbox" className="mt-1" {...props} />
      <label htmlFor={id} className="text-sm">
        <span className="font-medium">{label}</span>
        {description ? <span className="block text-xs text-ink-muted">{description}</span> : null}
      </label>
    </div>
  )
}
