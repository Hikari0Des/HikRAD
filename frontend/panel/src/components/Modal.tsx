import * as Dialog from '@radix-ui/react-dialog'
import { type ReactNode } from 'react'

import { useT } from '@hikrad/shared'

/**
 * App modal built on Radix Dialog: centered, scrollable, RTL-aware (logical
 * inset utilities), phone-width friendly (full-width down to 360 px). Titles
 * and content are supplied by the caller (already localized).
 */
export function Modal({
  open,
  onOpenChange,
  title,
  description,
  children,
  size = 'md',
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  title: string
  description?: string
  children: ReactNode
  size?: 'md' | 'lg'
}) {
  const t = useT()
  return (
    <Dialog.Root open={open} onOpenChange={onOpenChange}>
      <Dialog.Portal>
        <Dialog.Overlay className="fixed inset-0 z-40 bg-ink/40" />
        <Dialog.Content
          className={`fixed inset-x-0 top-8 z-50 mx-auto flex max-h-[85vh] w-[calc(100%-1.5rem)] flex-col rounded-lg bg-surface-raised shadow-xl ${
            size === 'lg' ? 'max-w-2xl' : 'max-w-md'
          }`}
        >
          <div className="flex items-start justify-between gap-4 border-b border-surface-sunken px-5 py-4">
            <div className="min-w-0">
              <Dialog.Title className="text-base font-semibold">{title}</Dialog.Title>
              {description ? (
                <Dialog.Description className="mt-0.5 text-sm text-ink-muted">
                  {description}
                </Dialog.Description>
              ) : (
                <Dialog.Description className="sr-only">{title}</Dialog.Description>
              )}
            </div>
            <Dialog.Close asChild>
              <button
                type="button"
                aria-label={t('ui.close')}
                className="rounded-md p-1.5 text-ink-muted hover:bg-surface-sunken"
              >
                <span aria-hidden="true">×</span>
              </button>
            </Dialog.Close>
          </div>
          <div className="min-h-0 flex-1 overflow-y-auto px-5 py-4">{children}</div>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  )
}
