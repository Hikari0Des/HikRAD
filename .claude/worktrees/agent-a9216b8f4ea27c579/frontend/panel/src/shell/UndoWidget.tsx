import { useEffect, useState } from 'react'

import { useT } from '@hikrad/shared'

import {
  isPendingApproval,
  listMyUndoable,
  undoAction,
  type UndoableAction,
} from '../api/accounting'
import { useAuth } from '../auth/AuthContext'
import { useOptionalToast } from '../components/Toast'
import { notifyBalanceChanged } from '../lib/balanceEvents'

const POLL_INTERVAL_MS = 15_000
const TICK_MS = 1_000

/**
 * FR-158: the five-minute undo window's top-bar widget. Shows the caller's
 * most recent not-yet-undone, not-yet-expired action with a live countdown;
 * one click undoes it (or, for a scoped agent, parks it for approval —
 * FR-159 — surfaced as a toast rather than a silent success).
 */
export function UndoWidget() {
  const t = useT()
  const { manager } = useAuth()
  const toast = useOptionalToast()
  const [actions, setActions] = useState<UndoableAction[]>([])
  const [now, setNow] = useState(() => Date.now())
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    if (!manager) return
    let cancelled = false
    function load() {
      listMyUndoable()
        .then((res) => {
          if (!cancelled) setActions(res.items)
        })
        .catch(() => {
          /* best-effort */
        })
    }
    load()
    const pollId = setInterval(load, POLL_INTERVAL_MS)
    const tickId = setInterval(() => setNow(Date.now()), TICK_MS)
    return () => {
      cancelled = true
      clearInterval(pollId)
      clearInterval(tickId)
    }
  }, [manager])

  const current = actions.find((a) => new Date(a.expires_at).getTime() > now)
  if (!current) return null

  const secondsLeft = Math.max(0, Math.floor((new Date(current.expires_at).getTime() - now) / 1000))
  const mm = String(Math.floor(secondsLeft / 60)).padStart(2, '0')
  const ss = String(secondsLeft % 60).padStart(2, '0')

  async function handleUndo() {
    setBusy(true)
    try {
      const res = await undoAction(current!.id)
      if (isPendingApproval(res)) {
        toast?.(t('undo.parked'), 'ok')
      } else {
        toast?.(t('undo.done'), 'ok')
        notifyBalanceChanged()
      }
      setActions((prev) => prev.filter((a) => a.id !== current!.id))
    } catch (err) {
      toast?.(err instanceof Error ? err.message : t('common.error.body'), 'danger')
    } finally {
      setBusy(false)
    }
  }

  return (
    <button
      type="button"
      onClick={() => void handleUndo()}
      disabled={busy}
      className="flex items-center gap-1.5 rounded-md bg-surface-sunken px-2.5 py-1 text-sm hover:bg-brand-soft disabled:opacity-50"
      title={t(`undo.kind.${current.kind}`)}
    >
      <svg
        aria-hidden="true"
        width="14"
        height="14"
        viewBox="0 0 16 16"
        fill="none"
        stroke="currentColor"
        strokeWidth="2"
        strokeLinecap="round"
        strokeLinejoin="round"
      >
        <path d="M3 7h6a4 4 0 1 1 0 8H6M3 7l3-3M3 7l3 3" />
      </svg>
      <span>{t('undo.action')}</span>
      <span className="font-mono text-xs text-ink-muted" dir="ltr">
        {mm}:{ss}
      </span>
    </button>
  )
}
