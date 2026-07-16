/**
 * RADIUS debug tail (contract C6, FR-39). SSE over the authorize-decision feed,
 * filtered by username and/or NAS. Same hand-rolled fetch-stream technique as
 * the live-sessions and notifications feeds (EventSource can't send a bearer).
 */
import { API_BASE } from './client'
import { tokenStore } from '../auth/tokenStore'
import type { SseHandle } from './monitoring'

export interface ReplyAttribute {
  intent: string
  value: string
}

export interface DebugEvent {
  at: string
  username: string
  nas: string
  outcome: string
  reason: string
  checks: string[]
  /** The resolved nas_services instance (FR-62) this request landed on. */
  instance?: string
  /**
   * The accept's reply intents — what HikRAD told the router to do. Absent on a
   * reject. An `address_pool` here must name a real `/ip pool` on that router or
   * the login still fails ("no address from ip pool") despite the accept.
   */
  attributes?: ReplyAttribute[]
}

export function openDebugStream(
  filters: { username?: string; nas?: string },
  handlers: { onEvent: (e: DebugEvent) => void; onError?: () => void },
): SseHandle {
  const ctrl = new AbortController()
  const token = tokenStore.getAccessToken()
  const params = new URLSearchParams()
  if (filters.username) params.set('username', filters.username)
  if (filters.nas) params.set('nas', filters.nas)
  const qs = params.toString()

  void (async () => {
    try {
      const res = await fetch(`${API_BASE}/live/debug${qs ? `?${qs}` : ''}`, {
        headers: {
          Accept: 'text/event-stream',
          ...(token ? { Authorization: `Bearer ${token}` } : {}),
        },
        signal: ctrl.signal,
      })
      if (!res.ok || !res.body) {
        handlers.onError?.()
        return
      }
      const reader = res.body.getReader()
      const decoder = new TextDecoder()
      let buf = ''
      for (;;) {
        const { done, value } = await reader.read()
        if (done) break
        buf += decoder.decode(value, { stream: true })
        let idx: number
        while ((idx = buf.indexOf('\n\n')) !== -1) {
          const frame = buf.slice(0, idx)
          buf = buf.slice(idx + 2)
          const dataLine = frame.split('\n').find((l) => l.startsWith('data:'))
          if (!dataLine) continue
          try {
            handlers.onEvent(JSON.parse(dataLine.slice(5).trim()) as DebugEvent)
          } catch {
            /* ignore malformed frame */
          }
        }
      }
    } catch (err) {
      if (!(err instanceof DOMException && err.name === 'AbortError')) handlers.onError?.()
    }
  })()

  return { close: () => ctrl.abort() }
}
