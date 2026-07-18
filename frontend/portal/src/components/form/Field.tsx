import { type ReactNode } from 'react'

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
