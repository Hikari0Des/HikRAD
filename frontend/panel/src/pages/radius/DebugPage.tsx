import { useEffect, useRef, useState } from 'react'

import { Ltr, useFormatters, useT } from '@hikrad/shared'

import { openDebugStream, type DebugEvent } from '../../api/debug'
import { Button } from '../../components/Button'
import { TextInput } from '../../components/form'
import { PageHeader } from '../../components/PageHeader'

const MAX_ROWS = 500

/** RADIUS debug tail (FR-39): live authorize decisions, filterable, pausable. */
export function DebugPage() {
  const t = useT()
  const { formatDate } = useFormatters()
  const [username, setUsername] = useState('')
  const [nas, setNas] = useState('')
  const [applied, setApplied] = useState<{ username?: string; nas?: string }>({})
  const [events, setEvents] = useState<DebugEvent[]>([])
  const [paused, setPaused] = useState(false)
  const pausedRef = useRef(paused)
  pausedRef.current = paused

  useEffect(() => {
    const handle = openDebugStream(applied, {
      onEvent: (e) => {
        // Scroll-lock: while paused we drop incoming rows rather than reorder the
        // list under the operator's cursor.
        if (pausedRef.current) return
        setEvents((prev) => [e, ...prev].slice(0, MAX_ROWS))
      },
    })
    return () => handle.close()
  }, [applied])

  return (
    <section>
      <PageHeader title={t('debug.title')} subtitle={t('debug.subtitle')} />

      <div className="mb-3 flex flex-wrap items-end gap-2">
        <label className="text-xs">
          <span className="mb-1 block text-ink-muted">{t('debug.filterUser')}</span>
          <TextInput value={username} onChange={(e) => setUsername(e.target.value)} dir="ltr" />
        </label>
        <label className="text-xs">
          <span className="mb-1 block text-ink-muted">{t('debug.filterNas')}</span>
          <TextInput value={nas} onChange={(e) => setNas(e.target.value)} dir="ltr" />
        </label>
        <Button
          size="sm"
          variant="secondary"
          onClick={() =>
            setApplied({ username: username.trim() || undefined, nas: nas.trim() || undefined })
          }
        >
          {t('debug.apply')}
        </Button>
        <Button
          size="sm"
          variant={paused ? 'primary' : 'ghost'}
          onClick={() => setPaused((p) => !p)}
        >
          {paused ? t('debug.resume') : t('debug.pause')}
        </Button>
        <Button size="sm" variant="ghost" onClick={() => setEvents([])}>
          {t('debug.clear')}
        </Button>
      </div>

      <div className="overflow-x-auto rounded-md border border-surface-sunken">
        <table className="w-full min-w-[40rem] text-sm">
          <thead className="bg-surface-sunken/40 text-xs text-ink-muted">
            <tr>
              <Th>{t('debug.col.at')}</Th>
              <Th>{t('debug.col.user')}</Th>
              <Th>{t('debug.col.nas')}</Th>
              <Th>{t('debug.col.outcome')}</Th>
              <Th>{t('debug.col.reason')}</Th>
            </tr>
          </thead>
          <tbody>
            {events.map((e, i) => (
              <tr key={i} className="border-t border-surface-sunken/60">
                <td className="px-3 py-1.5 whitespace-nowrap text-ink-muted">{formatDate(e.at)}</td>
                <td className="px-3 py-1.5">
                  <Ltr>{e.username}</Ltr>
                </td>
                <td className="px-3 py-1.5">
                  <Ltr>{e.nas}</Ltr>
                </td>
                <td className="px-3 py-1.5">
                  <OutcomeBadge outcome={e.outcome} />
                </td>
                <td className="px-3 py-1.5 text-ink-muted">{localizedReason(e.reason, t)}</td>
              </tr>
            ))}
          </tbody>
        </table>
        {events.length === 0 ? (
          <p className="p-6 text-center text-sm text-ink-muted">{t('debug.waiting')}</p>
        ) : null}
      </div>
    </section>
  )
}

function OutcomeBadge({ outcome }: { outcome: string }) {
  const t = useT()
  const accept = outcome === 'accept' || outcome === 'ok'
  return (
    <span
      className={`rounded px-1.5 py-0.5 text-xs font-medium ${
        accept ? 'bg-ok/10 text-ok' : 'bg-danger/10 text-danger'
      }`}
    >
      {t(`debug.outcome.${accept ? 'accept' : 'reject'}`)}
    </span>
  )
}

/** Map a backend reason key to a localized label, falling back to the raw key. */
function localizedReason(
  reason: string,
  t: (k: string, p?: Record<string, string>) => string,
): string {
  if (!reason) return '—'
  const localized = t(`debug.reason.${reason}`)
  // useT returns the key itself when missing; show the raw reason then.
  return localized === `debug.reason.${reason}` ? reason : localized
}

function Th({ children }: { children?: React.ReactNode }) {
  return <th className="px-3 py-2 text-start font-medium">{children}</th>
}
