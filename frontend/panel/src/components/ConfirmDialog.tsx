import { useState } from 'react'

import { useT } from '@hikrad/shared'

import { Button } from './Button'
import { Modal } from './Modal'

/**
 * Confirmation modal. `onConfirm` may be async; while it runs the confirm
 * button shows a busy state and both buttons disable. Used for destructive or
 * outward-facing actions (delete, disconnect, disable-with-CoA).
 */
export function ConfirmDialog({
  open,
  onOpenChange,
  title,
  body,
  confirmLabel,
  destructive = false,
  onConfirm,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  title: string
  body: string
  confirmLabel?: string
  destructive?: boolean
  onConfirm: () => void | Promise<void>
}) {
  const t = useT()
  const [busy, setBusy] = useState(false)

  async function confirm() {
    setBusy(true)
    try {
      await onConfirm()
      onOpenChange(false)
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal open={open} onOpenChange={busy ? () => {} : onOpenChange} title={title}>
      <p className="text-sm text-ink-muted">{body}</p>
      <div className="mt-6 flex justify-end gap-2">
        <Button variant="ghost" disabled={busy} onClick={() => onOpenChange(false)}>
          {t('ui.cancel')}
        </Button>
        <Button variant={destructive ? 'danger' : 'primary'} disabled={busy} onClick={confirm}>
          {busy ? t('ui.working') : (confirmLabel ?? t('ui.confirm'))}
        </Button>
      </div>
    </Modal>
  )
}
