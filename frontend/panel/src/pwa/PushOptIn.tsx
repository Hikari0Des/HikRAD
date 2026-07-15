import { useEffect, useState } from 'react'

import { useT } from '@hikrad/shared'

import { getVapidPublicKey, subscribePush, unsubscribePush, urlBase64ToUint8Array } from './pushApi'

type Status = 'unsupported' | 'denied' | 'off' | 'on' | 'busy'

/**
 * Push opt-in (contract C4, task 6): a small control for the notification
 * center. Permission denial is handled quietly (edge case in the task
 * brief) — no error banner, just the control reverting to its "off" state.
 */
export function PushOptIn() {
  const t = useT()
  const [status, setStatus] = useState<Status>('off')

  useEffect(() => {
    if (!('serviceWorker' in navigator) || !('PushManager' in window)) {
      setStatus('unsupported')
      return
    }
    if (Notification.permission === 'denied') {
      setStatus('denied')
      return
    }
    navigator.serviceWorker.ready.then((reg) =>
      reg.pushManager.getSubscription().then((sub) => setStatus(sub ? 'on' : 'off')),
    )
  }, [])

  async function enable() {
    setStatus('busy')
    const permission = await Notification.requestPermission()
    if (permission !== 'granted') {
      setStatus(permission === 'denied' ? 'denied' : 'off')
      return
    }
    const key = await getVapidPublicKey()
    if (!key) {
      setStatus('off')
      return
    }
    const reg = await navigator.serviceWorker.ready
    const sub = await reg.pushManager.subscribe({
      userVisibleOnly: true,
      applicationServerKey: urlBase64ToUint8Array(key) as BufferSource,
    })
    const ok = await subscribePush(sub.toJSON())
    setStatus(ok ? 'on' : 'off')
  }

  async function disable() {
    setStatus('busy')
    const reg = await navigator.serviceWorker.ready
    const sub = await reg.pushManager.getSubscription()
    if (sub) {
      await unsubscribePush(sub.endpoint)
      await sub.unsubscribe()
    }
    setStatus('off')
  }

  if (status === 'unsupported' || status === 'denied') return null

  return (
    <button
      type="button"
      disabled={status === 'busy'}
      onClick={status === 'on' ? disable : enable}
      className="w-full border-t border-surface-sunken px-3 py-2 text-start text-xs text-ink-muted hover:bg-surface-sunken disabled:opacity-60"
    >
      {status === 'on' ? t('pwa.pushDisable') : t('pwa.pushEnable')}
    </button>
  )
}
