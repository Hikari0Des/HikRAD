import { describe, expect, it } from 'vitest'

import type { Notification } from '../api/monitoring'
import {
  initialNotificationState,
  MAX_NOTIFICATIONS,
  notificationReducer,
  unreadCount,
  type NotificationState,
} from './notificationReducer'

function n(over: Partial<Notification> = {}): Notification {
  return { type: 'info', state: 'ok', summary: 'hi', at: '2026-07-11T00:00:00Z', ...over }
}

function receive(state: NotificationState, notif: Notification): NotificationState {
  return notificationReducer(state, { type: 'receive', notification: notif })
}

describe('notificationReducer (FR-36)', () => {
  it('prepends received notifications and counts them unread', () => {
    let s = initialNotificationState
    s = receive(s, n({ summary: 'first' }))
    s = receive(s, n({ summary: 'second' }))
    expect(s.items.map((i) => i.summary)).toEqual(['second', 'first'])
    expect(unreadCount(s)).toBe(2)
  })

  it('does not lose earlier events when new ones arrive (survives route changes)', () => {
    let s = initialNotificationState
    for (let i = 0; i < 5; i++) s = receive(s, n({ summary: `e${i}` }))
    expect(s.items).toHaveLength(5)
  })

  it('raises a banner for critical (down) events and clears it on dismiss', () => {
    let s = receive(
      initialNotificationState,
      n({ type: 'nas_down', state: 'down', summary: 'NAS 1 down' }),
    )
    expect(s.banner?.summary).toBe('NAS 1 down')
    s = notificationReducer(s, { type: 'dismissBanner' })
    expect(s.banner).toBeNull()
  })

  it('non-critical events do not raise a banner', () => {
    const s = receive(initialNotificationState, n({ type: 'expiring_digest', state: 'ok' }))
    expect(s.banner).toBeNull()
  })

  it('markAllRead zeroes the unread count but keeps items', () => {
    let s = receive(initialNotificationState, n())
    s = receive(s, n())
    s = notificationReducer(s, { type: 'markAllRead' })
    expect(unreadCount(s)).toBe(0)
    expect(s.items).toHaveLength(2)
  })

  it('caps retained history at MAX_NOTIFICATIONS', () => {
    let s = initialNotificationState
    for (let i = 0; i < MAX_NOTIFICATIONS + 20; i++) s = receive(s, n({ summary: `x${i}` }))
    expect(s.items).toHaveLength(MAX_NOTIFICATIONS)
    // Newest kept, oldest dropped.
    expect(s.items[0].summary).toBe(`x${MAX_NOTIFICATIONS + 19}`)
  })

  it('clear resets to the initial state', () => {
    let s = receive(initialNotificationState, n({ state: 'down', type: 'nas_down' }))
    s = notificationReducer(s, { type: 'clear' })
    expect(s).toEqual(initialNotificationState)
  })
})
