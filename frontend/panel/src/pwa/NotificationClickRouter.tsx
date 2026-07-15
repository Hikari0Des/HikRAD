import { useEffect } from 'react'
import { useNavigate } from 'react-router-dom'

/**
 * Notification click-through (task 6): the service worker's
 * `notificationclick` handler focuses/opens a client and posts
 * `{type:'hikrad:notification-click', url}` — this routes the already-loaded
 * SPA to that url via the in-app router instead of a full navigation.
 */
export function NotificationClickRouter() {
  const navigate = useNavigate()

  useEffect(() => {
    if (!('serviceWorker' in navigator)) return
    function onMessage(event: MessageEvent) {
      if (event.data?.type === 'hikrad:notification-click' && typeof event.data.url === 'string') {
        navigate(event.data.url)
      }
    }
    navigator.serviceWorker.addEventListener('message', onMessage)
    return () => navigator.serviceWorker.removeEventListener('message', onMessage)
  }, [navigate])

  return null
}
