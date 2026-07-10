/**
 * Typed fetch wrapper for the HikRAD API — contract C2 (00-phase.md):
 * base /api/v1, JSON, error envelope
 * `{"error":{"code","message","field_errors":[{"field","message"}]}}`,
 * cursor pagination `?cursor&limit` → `{"items":[…],"next_cursor":"…|null"}`,
 * `Authorization: Bearer <access-token>`.
 *
 * On 401 the refresh stub is consulted (Phase-2 internals swap in
 * transparently); if it cannot recover, tokens are cleared and
 * UNAUTHORIZED_EVENT fires so the router redirects to /login.
 */
import { tokenStore } from '../auth/tokenStore'
import { tryRefresh } from '../auth/refresh'

export const API_BASE = '/api/v1'

/** Fired on an unrecoverable 401; AuthProvider listens and drops the session. */
export const UNAUTHORIZED_EVENT = 'hikrad:unauthorized'

export interface FieldError {
  field: string
  message: string
}

interface ErrorEnvelope {
  error: {
    code: string
    message: string
    field_errors?: FieldError[]
  }
}

/** A structured API error parsed from the C2 envelope. */
export class ApiError extends Error {
  constructor(
    readonly status: number,
    readonly code: string,
    message: string,
    readonly fieldErrors: FieldError[] = [],
  ) {
    super(message)
    this.name = 'ApiError'
  }
}

/** The server could not be reached at all (backend down, no network). */
export class NetworkError extends Error {
  constructor(cause?: unknown) {
    super('network request failed')
    this.name = 'NetworkError'
    this.cause = cause
  }
}

export interface RequestOptions {
  method?: 'GET' | 'POST' | 'PUT' | 'PATCH' | 'DELETE'
  body?: unknown
  query?: Record<string, string | number | undefined>
  signal?: AbortSignal
}

function buildUrl(path: string, query?: RequestOptions['query']): string {
  let url = API_BASE + path
  if (query) {
    const params = new URLSearchParams()
    for (const [key, value] of Object.entries(query)) {
      if (value !== undefined) params.set(key, String(value))
    }
    const qs = params.toString()
    if (qs) url += `?${qs}`
  }
  return url
}

function parseEnvelope(status: number, payload: unknown): ApiError {
  if (
    typeof payload === 'object' &&
    payload !== null &&
    typeof (payload as ErrorEnvelope).error === 'object' &&
    (payload as ErrorEnvelope).error !== null
  ) {
    const { code, message, field_errors } = (payload as ErrorEnvelope).error
    return new ApiError(status, code ?? 'unknown', message ?? '', field_errors ?? [])
  }
  return new ApiError(status, 'unknown', `unexpected response (HTTP ${status})`)
}

export async function request<T>(path: string, options: RequestOptions = {}): Promise<T> {
  const headers: Record<string, string> = { Accept: 'application/json' }
  const token = tokenStore.getAccessToken()
  if (token) headers['Authorization'] = `Bearer ${token}`
  if (options.body !== undefined) headers['Content-Type'] = 'application/json'

  let res: Response
  try {
    res = await fetch(buildUrl(path, options.query), {
      method: options.method ?? (options.body !== undefined ? 'POST' : 'GET'),
      headers,
      body: options.body !== undefined ? JSON.stringify(options.body) : undefined,
      signal: options.signal,
    })
  } catch (err) {
    if (err instanceof DOMException && err.name === 'AbortError') throw err
    throw new NetworkError(err)
  }

  if (res.status === 401 && !path.startsWith('/auth/')) {
    const recovered = await tryRefresh()
    if (recovered) return request<T>(path, options)
    tokenStore.clear()
    window.dispatchEvent(new Event(UNAUTHORIZED_EVENT))
  }

  if (!res.ok) {
    let payload: unknown
    try {
      payload = await res.json()
    } catch {
      payload = undefined
    }
    throw parseEnvelope(res.status, payload)
  }

  if (res.status === 204) return undefined as T
  return (await res.json()) as T
}

/** C2 list-endpoint page shape. */
export interface Page<T> {
  items: T[]
  next_cursor: string | null
}

export interface PageParams {
  cursor?: string
  limit?: number
}

/** Fetch one page of a C2 list endpoint, optionally with extra filter params. */
export function listPage<T>(
  path: string,
  params: PageParams = {},
  extraQuery: Record<string, string | number | undefined> = {},
): Promise<Page<T>> {
  return request<Page<T>>(path, {
    query: { ...extraQuery, cursor: params.cursor, limit: params.limit },
  })
}

/** Walk a C2 list endpoint across cursors, yielding every item. */
export async function* paginate<T>(path: string, limit?: number): AsyncGenerator<T> {
  let cursor: string | undefined
  do {
    const page = await listPage<T>(path, { cursor, limit })
    yield* page.items
    cursor = page.next_cursor ?? undefined
  } while (cursor)
}
