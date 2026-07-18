/**
 * One-click updater relay (v2 phase 7, FR-86/87). Mirrors
 * backend/internal/updates' response shapes exactly.
 *
 * The SSE stream is auth-gated (Bearer), so — same reasoning as api/live.ts
 * — this uses fetch + a line parser instead of the browser `EventSource`,
 * which can't send an Authorization header.
 */
import { API_BASE, request } from './client'
import { tokenStore } from '../auth/tokenStore'

export interface UpdateCheckResult {
  ok: boolean
  current_version: string
  available_version: string | null
  delivery_mode: string
  bundle_path: string | null
}

export function checkForUpdate(): Promise<UpdateCheckResult> {
  return request<UpdateCheckResult>('/system/update/check')
}

export function startUpdate(bundlePath?: string | null): Promise<{ status: string }> {
  return request<{ status: string }>('/system/update', {
    method: 'POST',
    body: { bundle_path: bundlePath ?? undefined },
  })
}

export interface UpdateStatus {
  ok: boolean
  locked: boolean
  lock_owner: string | null
  stage: string
  started_at: string | null
  last_action?: string
  result?: string
  version?: string
  completed_at?: string
}

export function getUpdateStatus(): Promise<UpdateStatus> {
  return request<UpdateStatus>('/system/update/status')
}

// --- SSE progress stream (C4) -----------------------------------------------

export type UpdateStreamEvent =
  | { type: 'progress'; stage: string; ts?: string }
  | { type: 'done'; version?: string; message?: string }
  | { type: 'rolled_back'; version?: string; message?: string }

export interface UpdateStreamHandle {
  close: () => void
}

/**
 * Open the update progress stream. Unlike the live-sessions feed (api/live.ts),
 * this does NOT auto-reconnect forever — a `done`/`rolled_back` event is
 * terminal by design (the update is over). It reconnects exactly once on an
 * unexpected drop (the panel's own container being replaced mid-update is
 * the expected case this exists for, FR-87.2) and then stops; the caller is
 * expected to poll getUpdateStatus() afterwards to reconcile if the second
 * connection also fails to resolve things.
 */
export function openUpdateStream(
  onEvent: (evt: UpdateStreamEvent) => void,
  onDisconnect?: () => void,
): UpdateStreamHandle {
  let closed = false
  let reconnected = false
  const controller = new AbortController()

  const run = async (): Promise<void> => {
    try {
      const headers: Record<string, string> = { Accept: 'text/event-stream' }
      const token = tokenStore.getAccessToken()
      if (token) headers['Authorization'] = `Bearer ${token}`
      const res = await fetch(`${API_BASE}/system/update/stream`, {
        headers,
        signal: controller.signal,
      })
      if (!res.ok || !res.body) throw new Error(`update stream HTTP ${res.status}`)
      await pump(res.body, onEvent, () => closed)
    } catch {
      // Fall through to the reconnect-once logic below unless deliberately closed.
    }
    if (closed) return
    onDisconnect?.()
    if (!reconnected) {
      reconnected = true
      void run()
    }
  }
  void run()

  return {
    close() {
      closed = true
      controller.abort()
    },
  }
}

async function pump(
  body: ReadableStream<Uint8Array>,
  onEvent: (evt: UpdateStreamEvent) => void,
  isClosed: () => boolean,
): Promise<void> {
  const reader = body.getReader()
  const decoder = new TextDecoder()
  let buffer = ''
  for (;;) {
    const { value, done } = await reader.read()
    if (done || isClosed()) break
    buffer += decoder.decode(value, { stream: true })
    let sep: number
    while ((sep = buffer.indexOf('\n\n')) !== -1) {
      const frame = buffer.slice(0, sep)
      buffer = buffer.slice(sep + 2)
      const evt = parseFrame(frame)
      if (evt) onEvent(evt)
    }
  }
}

export function parseFrame(frame: string): UpdateStreamEvent | null {
  let evtName = ''
  const dataLines: string[] = []
  for (const line of frame.split('\n')) {
    if (line.startsWith(':')) continue // comment / ping
    if (line.startsWith('event:')) evtName = line.slice(6).trim()
    else if (line.startsWith('data:')) dataLines.push(line.slice(5).trim())
  }
  if (!evtName || dataLines.length === 0) return null
  let data: unknown
  try {
    data = JSON.parse(dataLines.join('\n'))
  } catch {
    return null
  }
  const d = data as { stage?: string; ts?: string; version?: string; message?: string }
  if (evtName === 'progress') return { type: 'progress', stage: d.stage ?? '', ts: d.ts }
  if (evtName === 'done') return { type: 'done', version: d.version, message: d.message }
  if (evtName === 'rolled_back')
    return { type: 'rolled_back', version: d.version, message: d.message }
  return null
}
