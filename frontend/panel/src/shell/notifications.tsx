import { createContext, useContext, useEffect, useReducer, useState, type ReactNode } from 'react'

import { useFormatters, useT } from '@hikrad/shared'

import { openNotificationStream } from '../api/monitoring'
import {
  initialNotificationState,
  notificationReducer,
  unreadCount,
  type NotificationState,
} from '../lib/notificationReducer'
import { PushOptIn } from '../pwa/PushOptIn'

interface NotificationContextValue {
  state: NotificationState
  markAllRead: () => void
  dismissBanner: () => void
}

const NotificationContext = createContext<NotificationContextValue | null>(null)

/**
 * Notification store + SSE subscription (FR-36). Mounted once inside the app
 * shell — above the router Outlet — so events survive route changes. The
 * notifications SSE dispatches into the reducer; a reconnect loop keeps the feed
 * alive across transient drops.
 */
export function NotificationProvider({ children }: { children: ReactNode }) {
  const [state, dispatch] = useReducer(notificationReducer, initialNotificationState)

  useEffect(() => {
    let handle: { close: () => void } = { close: () => {} }
    let reconnectTimer = 0
    let closed = false

    function connect() {
      if (closed) return
      handle = openNotificationStream({
        onNotification: (n) => dispatch({ type: 'receive', notification: n }),
        onError: () => {
          // Reconnect after a short delay so a NAS-down alert isn't missed.
          reconnectTimer = window.setTimeout(connect, 5000)
        },
      })
    }
    connect()

    return () => {
      closed = true
      window.clearTimeout(reconnectTimer)
      handle.close()
    }
  }, [])

  return (
    <NotificationContext.Provider
      value={{
        state,
        markAllRead: () => dispatch({ type: 'markAllRead' }),
        dismissBanner: () => dispatch({ type: 'dismissBanner' }),
      }}
    >
      {children}
    </NotificationContext.Provider>
  )
}

export function useNotifications(): NotificationContextValue {
  const ctx = useContext(NotificationContext)
  if (!ctx) throw new Error('useNotifications must be used inside <NotificationProvider>')
  return ctx
}

/** Bell with unread badge + a dropdown notification center. */
export function NotificationBell() {
  const t = useT()
  const { formatDate } = useFormatters()
  const { state, markAllRead } = useNotifications()
  const [open, setOpen] = useState(false)
  const unread = unreadCount(state)

  return (
    <div className="relative">
      <button
        type="button"
        aria-label={t('notifications.title')}
        onClick={() => {
          setOpen((o) => !o)
          if (!open) markAllRead()
        }}
        className="relative rounded-md p-2 hover:bg-surface-sunken"
      >
        <span aria-hidden="true">🔔</span>
        {unread > 0 ? (
          <span className="absolute end-0.5 top-0.5 inline-flex h-4 min-w-4 items-center justify-center rounded-full bg-danger px-1 text-[10px] font-bold text-ink-inverse">
            {unread}
          </span>
        ) : null}
      </button>
      {open ? (
        <div className="absolute end-0 z-30 mt-1 max-h-96 w-80 overflow-y-auto rounded-md border border-surface-sunken bg-surface-raised shadow-lg">
          <div className="border-b border-surface-sunken px-3 py-2 text-sm font-semibold">
            {t('notifications.title')}
          </div>
          {state.items.length === 0 ? (
            <p className="p-4 text-center text-sm text-ink-muted">{t('notifications.empty')}</p>
          ) : (
            <ul>
              {state.items.map((n) => (
                <li key={n.id} className="border-b border-surface-sunken/60 px-3 py-2 text-sm">
                  <p className="font-medium">{n.summary}</p>
                  <p className="mt-0.5 text-xs text-ink-muted">{formatDate(n.at)}</p>
                </li>
              ))}
            </ul>
          )}
          {/* Push opt-in (contract C4, Phase-4 task 6 — Agent F, src/pwa/**
              exception scope; this one-line mount is the only change to this
              file). */}
          <PushOptIn />
        </div>
      ) : null}
    </div>
  )
}

/** Full-width critical banner (e.g. NAS down) until dismissed. */
export function CriticalBanner() {
  const { state, dismissBanner } = useNotifications()
  const t = useT()
  if (!state.banner) return null
  return (
    <div
      role="alert"
      className="flex items-center gap-3 bg-danger px-4 py-2 text-sm text-ink-inverse"
    >
      <span className="min-w-0 flex-1 break-words">{state.banner.summary}</span>
      <button
        type="button"
        onClick={dismissBanner}
        aria-label={t('ui.close')}
        className="opacity-80 hover:opacity-100"
      >
        <span aria-hidden="true">×</span>
      </button>
    </div>
  )
}
