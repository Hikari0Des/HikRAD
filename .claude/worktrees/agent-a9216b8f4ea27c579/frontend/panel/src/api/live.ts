/**
 * Live sessions, history, usage graphs and CoA disconnect (contracts C6/C7-C).
 *
 * The SSE feed is auth-gated (Bearer), so we cannot use the browser
 * `EventSource` (it can't send an Authorization header). Instead we stream the
 * response body with fetch + a small line parser, which also lets us abort
 * cleanly on unmount and reconnect with a fresh snapshot.
 */
import { API_BASE, listPage, request, type Page } from './client'
import { tokenStore } from '../auth/tokenStore'
import type { DisconnectResult, LiveSession, SessionHistory, UsagePoint } from './types'

export interface LiveFilter {
  nas_id?: string
  profile_id?: string
  manager_id?: string
  q?: string
  /** FR-61/63: the SESSION's service (pppoe|hotspot), not the subscriber's type. */
  service?: string
}

export function listSessionHistory(params: {
  subscriber_id?: string
  cursor?: string
  limit?: number
}): Promise<Page<SessionHistory>> {
  return listPage<SessionHistory>('/sessions', params)
}

export function usageBySubscriber(
  id: string,
  granularity: 'daily' | 'monthly',
  range?: { from?: string; to?: string },
): Promise<UsagePoint[]> {
  return request<UsagePoint[]>(`/usage/subscriber/${id}`, {
    query: { granularity, from: range?.from, to: range?.to },
  })
}

/**
 * Disconnect a live session via CoA (FR-31.3). v4-8: enqueue-only — the
 * server replies 202 with the coa_outbox row id; the actual ack/nak/timeout
 * (and the hybrid fallback, if it comes to that) resolves asynchronously on
 * the dispatcher's worker tick and shows up on the CoA Outbox screen.
 */
export function disconnectSession(nasId: string, acctSessionId: string): Promise<DisconnectResult> {
  return request('/live/disconnect', {
    method: 'POST',
    body: { nas_id: nasId, acct_session_id: acctSessionId },
  })
}

// --- SSE stream -----------------------------------------------------------

export type LiveEvent =
  | { type: 'snapshot'; sessions: LiveSession[] }
  | { type: 'upsert'; session: LiveSession }
  | { type: 'remove'; field: string }

export interface LiveStreamHandle {
  close: () => void
}

interface LiveStreamCallbacks {
  onEvent: (evt: LiveEvent) => void
  /** Fired when the connection drops so the caller can show a reconnecting state. */
  onDisconnect?: () => void
  /** Fired once a stream is (re)established and the snapshot has arrived. */
  onConnect?: () => void
}

function buildLiveUrl(filter: LiveFilter): string {
  const params = new URLSearchParams()
  for (const [k, v] of Object.entries(filter)) {
    if (v) params.set(k, v)
  }
  const qs = params.toString()
  return `${API_BASE}/live/sessions${qs ? `?${qs}` : ''}`
}

/**
 * Open the live-sessions stream. Auto-reconnects with backoff; every reconnect
 * re-issues the request, so the server replays a fresh `snapshot` and the
 * caller's reducer re-syncs (no ghost rows). Returns a handle whose close()
 * stops reconnecting and aborts the in-flight request.
 */
export function openLiveStream(filter: LiveFilter, cb: LiveStreamCallbacks): LiveStreamHandle {
  let closed = false
  let controller: AbortController | null = null
  let retryDelay = 1000

  const run = async () => {
    while (!closed) {
      controller = new AbortController()
      try {
        const headers: Record<string, string> = { Accept: 'text/event-stream' }
        const token = tokenStore.getAccessToken()
        if (token) headers['Authorization'] = `Bearer ${token}`
        const res = await fetch(buildLiveUrl(filter), {
          headers,
          signal: controller.signal,
        })
        if (!res.ok || !res.body) {
          throw new Error(`live stream HTTP ${res.status}`)
        }
        cb.onConnect?.()
        retryDelay = 1000
        await pump(res.body, cb.onEvent, () => closed)
      } catch {
        // Fall through to reconnect unless we were deliberately closed.
      }
      if (closed) break
      cb.onDisconnect?.()
      await sleep(retryDelay)
      retryDelay = Math.min(retryDelay * 2, 15000)
    }
  }
  void run()

  return {
    close() {
      closed = true
      controller?.abort()
    },
  }
}

async function pump(
  body: ReadableStream<Uint8Array>,
  onEvent: (evt: LiveEvent) => void,
  isClosed: () => boolean,
): Promise<void> {
  const reader = body.getReader()
  const decoder = new TextDecoder()
  let buffer = ''
  for (;;) {
    const { value, done } = await reader.read()
    if (done || isClosed()) break
    buffer += decoder.decode(value, { stream: true })
    // Events are separated by a blank line.
    let sep: number
    while ((sep = buffer.indexOf('\n\n')) !== -1) {
      const frame = buffer.slice(0, sep)
      buffer = buffer.slice(sep + 2)
      const evt = parseFrame(frame)
      if (evt) onEvent(evt)
    }
  }
}

/** Parse one SSE frame (`event:` + `data:` lines). Ping comments are ignored. */
export function parseFrame(frame: string): LiveEvent | null {
  let event = ''
  const dataLines: string[] = []
  for (const line of frame.split('\n')) {
    if (line.startsWith(':')) continue // comment / ping
    if (line.startsWith('event:')) event = line.slice(6).trim()
    else if (line.startsWith('data:')) dataLines.push(line.slice(5).trim())
  }
  if (!event || dataLines.length === 0) return null
  let data: unknown
  try {
    data = JSON.parse(dataLines.join('\n'))
  } catch {
    return null
  }
  if (event === 'snapshot') return { type: 'snapshot', sessions: (data as LiveSession[]) ?? [] }
  if (event === 'upsert') return { type: 'upsert', session: data as LiveSession }
  if (event === 'remove') return { type: 'remove', field: (data as { field: string }).field }
  return null
}

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms))
}
