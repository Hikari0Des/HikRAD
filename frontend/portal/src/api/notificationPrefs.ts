/** Notification-channel preferences (FR-170). The write requires full scope
 * (FR-167 §4: notification channels are one of the four full-scope-only
 * actions) — a reduced-scope PUT gets 403 server-side regardless of what
 * this UI renders. */
import { request } from './client'

export interface NotificationPrefs {
  sms: boolean
  email: boolean
  whatsapp: boolean
}

export function getNotificationPrefs(): Promise<NotificationPrefs> {
  return request<NotificationPrefs>('/portal/notification-preferences')
}

export function updateNotificationPrefs(prefs: NotificationPrefs): Promise<NotificationPrefs> {
  return request<NotificationPrefs>('/portal/notification-preferences', {
    method: 'PUT',
    body: prefs,
  })
}
