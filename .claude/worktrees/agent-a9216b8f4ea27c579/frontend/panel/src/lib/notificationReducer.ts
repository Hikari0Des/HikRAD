/**
 * Notification-center state (FR-36). The in-app notification feed is driven by
 * the notifications SSE; this reducer is the single source of truth so events
 * survive route changes (the store lives above the router in the app shell, the
 * SSE handler dispatches into it). Kept pure and framework-free so it is unit
 * testable in isolation.
 */
import type { Notification } from '../api/monitoring'

export interface NotificationItem extends Notification {
  /** Stable client id (SSE frames carry no id). */
  id: string
  read: boolean
}

export interface NotificationState {
  items: NotificationItem[]
  /** Latest unacknowledged critical event, drives the top banner (NAS down). */
  banner: NotificationItem | null
}

export type NotificationAction =
  | { type: 'receive'; notification: Notification }
  | { type: 'markAllRead' }
  | { type: 'dismissBanner' }
  | { type: 'clear' }

/** Cap retained history so a long session can't grow unbounded. */
export const MAX_NOTIFICATIONS = 100

export const initialNotificationState: NotificationState = { items: [], banner: null }

/** A notification is banner-worthy when it reports something going down. */
function isCritical(n: Notification): boolean {
  return n.state === 'down' || n.type === 'nas_down' || n.type === 'device_down'
}

let seq = 0
function nextId(): string {
  seq += 1
  return `n${seq}-${Date.now()}`
}

export function notificationReducer(
  state: NotificationState,
  action: NotificationAction,
): NotificationState {
  switch (action.type) {
    case 'receive': {
      const item: NotificationItem = { ...action.notification, id: nextId(), read: false }
      const items = [item, ...state.items].slice(0, MAX_NOTIFICATIONS)
      return {
        items,
        banner: isCritical(action.notification) ? item : state.banner,
      }
    }
    case 'markAllRead':
      return {
        ...state,
        items: state.items.map((i) => (i.read ? i : { ...i, read: true })),
      }
    case 'dismissBanner':
      return { ...state, banner: null }
    case 'clear':
      return initialNotificationState
    default:
      return state
  }
}

/** Count of unread items (badge on the notification bell). */
export function unreadCount(state: NotificationState): number {
  return state.items.reduce((n, i) => (i.read ? n : n + 1), 0)
}
