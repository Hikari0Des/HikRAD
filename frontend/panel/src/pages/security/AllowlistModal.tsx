import { useState } from 'react'

import { ErrorState, LoadingState, useT } from '@hikrad/shared'

import { getManagerAllowlist, putManagerAllowlist, type AllowlistEntry } from '../../api/security'
import type { ManagerView } from '../../api/managers'
import { Button } from '../../components/Button'
import { TextInput } from '../../components/form'
import { Modal } from '../../components/Modal'
import { useToast } from '../../components/Toast'
import { useAsync } from '../../hooks/useAsync'

/**
 * Per-manager IP allowlist editor (FR-30). An empty list means "any network".
 * A non-empty list restricts logins — hence the self-lockout warning: an admin
 * editing their own allowlist from an excluded network would be locked out.
 */
export function AllowlistModal({
  manager,
  onClose,
}: {
  manager: ManagerView
  onClose: () => void
}) {
  const t = useT()
  const { toast } = useToast()
  const load = useAsync(() => getManagerAllowlist(manager.id), [manager.id])
  const [entries, setEntries] = useState<AllowlistEntry[] | null>(null)
  const [busy, setBusy] = useState(false)

  const rows = entries ?? load.data?.entries ?? []

  function update(next: AllowlistEntry[]) {
    setEntries(next)
  }

  async function save() {
    setBusy(true)
    try {
      await putManagerAllowlist(
        manager.id,
        rows.filter((r) => r.cidr.trim() !== ''),
      )
      toast(t('allowlist.saved'), 'ok')
      onClose()
    } catch (err) {
      toast(err instanceof Error ? err.message : t('common.error.body'), 'danger')
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal
      open
      onOpenChange={busy ? () => {} : (o) => !o && onClose()}
      title={t('allowlist.title', { name: manager.username })}
      size="lg"
    >
      {load.error ? (
        <ErrorState onRetry={load.reload} />
      ) : load.loading ? (
        <LoadingState />
      ) : (
        <div className="space-y-4">
          <div className="rounded-md bg-warn/10 p-3 text-sm text-warn">
            {t('allowlist.warning')}
          </div>
          {rows.length === 0 ? (
            <p className="text-sm text-ink-muted">{t('allowlist.emptyMeansAny')}</p>
          ) : (
            <ul className="space-y-2">
              {rows.map((row, i) => (
                <li key={i} className="flex items-center gap-2">
                  <TextInput
                    value={row.cidr}
                    onChange={(e) =>
                      update(rows.map((r, j) => (j === i ? { ...r, cidr: e.target.value } : r)))
                    }
                    placeholder={t('allowlist.cidrPlaceholder')}
                    dir="ltr"
                    className="font-mono"
                  />
                  <TextInput
                    value={row.note}
                    onChange={(e) =>
                      update(rows.map((r, j) => (j === i ? { ...r, note: e.target.value } : r)))
                    }
                    placeholder={t('allowlist.notePlaceholder')}
                  />
                  <Button
                    size="sm"
                    variant="ghost"
                    onClick={() => update(rows.filter((_, j) => j !== i))}
                    aria-label={t('ui.remove')}
                  >
                    ×
                  </Button>
                </li>
              ))}
            </ul>
          )}
          <Button
            size="sm"
            variant="secondary"
            onClick={() => update([...rows, { cidr: '', note: '' }])}
          >
            {t('allowlist.add')}
          </Button>
          <div className="flex justify-end gap-2 border-t border-surface-sunken pt-3">
            <Button variant="ghost" disabled={busy} onClick={onClose}>
              {t('ui.cancel')}
            </Button>
            <Button disabled={busy} onClick={() => void save()}>
              {busy ? t('ui.working') : t('ui.save')}
            </Button>
          </div>
        </div>
      )}
    </Modal>
  )
}
