import { useState } from 'react'

import { useFormatters, useT } from '@hikrad/shared'

import { nasSnippet, nasStatus } from '../../api/nas'
import type { Nas } from '../../api/types'
import { Button } from '../../components/Button'
import { CopyButton } from '../../components/CopyButton'
import { Modal } from '../../components/Modal'
import { useAsync } from '../../hooks/useAsync'
import { useToast } from '../../components/Toast'

/**
 * Copy-paste RouterOS bootstrap (FR-14.1). ROS 6/7 are tabbed; the snippet block
 * is `<pre>` (machine text, LTR) with a copy button. "Test" hits the seen-since-
 * created status endpoint (FR-14.4) and reports whether the router has checked
 * in yet.
 */
export function SnippetModal({
  nas,
  open,
  onOpenChange,
}: {
  nas: Nas
  open: boolean
  onOpenChange: (o: boolean) => void
}) {
  const t = useT()
  const { formatDate } = useFormatters()
  const { toast } = useToast()
  const [ros, setRos] = useState<'6' | '7'>(nas.ros_version === '6' ? '6' : '7')

  const { data, error, loading, reload } = useAsync(() => nasSnippet(nas.id, ros), [nas.id, ros])

  async function test() {
    try {
      const status = await nasStatus(nas.id)
      if (status.seen) {
        const when = status.last_auth_at ?? status.last_acct_at
        toast(t('nas.testSeen', { when: when ? formatDate(when) : '' }), 'ok')
      } else {
        toast(t('nas.testUnseen'), 'danger')
      }
    } catch (err) {
      toast(err instanceof Error ? err.message : t('common.error.body'), 'danger')
    }
  }

  return (
    <Modal
      open={open}
      onOpenChange={onOpenChange}
      size="lg"
      title={t('nas.snippetTitle', { name: nas.name })}
      description={t('nas.snippetHint')}
    >
      <div className="mb-3 inline-flex overflow-hidden rounded-md border border-surface-sunken text-sm">
        <button
          type="button"
          onClick={() => setRos('7')}
          className={`px-4 py-1.5 ${ros === '7' ? 'bg-brand text-ink-inverse' : ''}`}
        >
          {t('nas.rosTab', { v: 7 })}
        </button>
        <button
          type="button"
          onClick={() => setRos('6')}
          className={`px-4 py-1.5 ${ros === '6' ? 'bg-brand text-ink-inverse' : ''}`}
        >
          {t('nas.rosTab', { v: 6 })}
        </button>
      </div>

      {error ? (
        <p className="text-sm text-danger">{t('common.error.body')}</p>
      ) : loading ? (
        <p className="text-sm text-ink-muted">{t('common.loading')}</p>
      ) : (
        <div className="relative">
          <div className="absolute end-2 top-2">
            <CopyButton text={data?.snippet ?? ''} />
          </div>
          <pre
            dir="ltr"
            className="max-h-80 overflow-auto rounded-md bg-ink/90 p-4 text-xs leading-relaxed text-ink-inverse"
          >
            {data?.snippet}
          </pre>
        </div>
      )}

      <div className="mt-4 flex items-center justify-between gap-2">
        <Button variant="ghost" size="sm" onClick={reload}>
          {t('ui.refresh')}
        </Button>
        <Button variant="secondary" size="sm" onClick={test}>
          {t('nas.test')}
        </Button>
      </div>
    </Modal>
  )
}
