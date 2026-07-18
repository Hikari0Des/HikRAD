import { useId, useState, type ChangeEvent, type DragEvent, type ReactNode } from 'react'

/**
 * Styled file upload with drag-and-drop (FR-94, new control — no
 * pre-modernization equivalent). The real `<input type="file">` stays in
 * the DOM (`sr-only`, not hidden via `display:none`) so it keeps native
 * keyboard/screen-reader behavior; the visible drop-zone is a `<label>`
 * wrapping it, matching the native label-click-activates-input pattern
 * every browser already supports, no JS click-forwarding needed. Not
 * CI-gated (C4's negative scope is select/checkbox/radio only) — a native
 * file input has no OS-chrome popup the way those three do.
 */
export function FileInput({
  label,
  hint,
  accept,
  multiple,
  onFilesSelected,
  disabled,
  className,
}: {
  label: ReactNode
  hint?: ReactNode
  accept?: string
  multiple?: boolean
  onFilesSelected: (files: FileList | null) => void
  disabled?: boolean
  className?: string
}) {
  const id = useId()
  const [dragOver, setDragOver] = useState(false)

  return (
    <div className={className}>
      <label
        htmlFor={id}
        onDragOver={(e: DragEvent<HTMLLabelElement>) => {
          e.preventDefault()
          if (!disabled) setDragOver(true)
        }}
        onDragLeave={() => setDragOver(false)}
        onDrop={(e: DragEvent<HTMLLabelElement>) => {
          e.preventDefault()
          setDragOver(false)
          if (!disabled) onFilesSelected(e.dataTransfer.files)
        }}
        className={`flex cursor-pointer flex-col items-center justify-center gap-1 rounded-md border-2 border-dashed px-3 py-4 text-center text-sm transition-colors motion-reduce:transition-none has-[:focus-visible]:ring-2 has-[:focus-visible]:ring-brand has-[:focus-visible]:ring-offset-1 has-[:focus-visible]:ring-offset-surface ${
          dragOver ? 'border-brand bg-brand-soft' : 'border-surface-sunken bg-surface'
        } ${disabled ? 'cursor-not-allowed opacity-60' : ''}`}
      >
        <span className="font-medium">{label}</span>
        {hint ? <span className="text-xs text-ink-muted">{hint}</span> : null}
        <input
          id={id}
          type="file"
          accept={accept}
          multiple={multiple}
          disabled={disabled}
          className="sr-only"
          onChange={(e: ChangeEvent<HTMLInputElement>) => onFilesSelected(e.target.files)}
        />
      </label>
    </div>
  )
}
