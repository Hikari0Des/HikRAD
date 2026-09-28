/**
 * Thin fetch helper for the HikRAD REST API (contract C2, consumed by the
 * portal now and available to any app): base /api/v1, JSON bodies, the frozen
 * error envelope, cursor pagination, bearer-token injection and a 401 hook.
 * Deliberately minimal — richer per-app clients (panel) stay app-local.
 */
export interface FieldError {
  field: string
  message: string
}

/** The frozen C2 error envelope, thrown as a typed error. */
export class ApiError extends Error {
  readonly code: string
  readonly status: number
  readonly fieldErrors: FieldError[]

  constructor(code: string, message: string, status: number, fieldErrors: FieldError[] = []) {
    super(message)
    this.name = 'ApiError'
    this.code = code
    this.status = status
    this.fieldErrors = fieldErrors
  }
}

/** C2 list-endpoint shape: `{"items":[…],"next_cursor":"…|null"}`. */
export interface Page<T> {
  items: T[]
  next_cursor: string | null
}

export interface ListParams {
  cursor?: string
  /** C2: server caps at 100. */
  limit?: number
}

export interface ApiClientOptions {
  /** Defaults to '/api/v1' (Caddy proxies /api/* to hikrad-api, contract C5). */
  baseUrl?: string
  /** Returns the current access token, or null when signed out. */
  getToken?: () => string | null
  /** Called on any 401 before the error is thrown (e.g. redirect to login). */
  onUnauthorized?: () => void
  /** Test seam. */
  fetchFn?: typeof fetch
}

interface RequestOptions {
  body?: unknown
  query?: Record<string, string | number | boolean | undefined>
}

interface ErrorEnvelope {
  error?: { code?: string; message?: string; field_errors?: FieldError[] }
}

export function createApiClient(options: ApiClientOptions = {}) {
  const { baseUrl = '/api/v1', getToken, onUnauthorized, fetchFn } = options

  async function request<T>(method: string, path: string, opts: RequestOptions = {}): Promise<T> {
    const url = new URL(baseUrl + path, window.location.origin)
    for (const [key, value] of Object.entries(opts.query ?? {})) {
      if (value !== undefined) url.searchParams.set(key, String(value))
    }

    const headers: Record<string, string> = { Accept: 'application/json' }
    if (opts.body !== undefined) headers['Content-Type'] = 'application/json'
    const token = getToken?.()
    if (token) headers.Authorization = `Bearer ${token}`

    let response: Response
    try {
      const doFetch = fetchFn ?? fetch
      response = await doFetch(url.toString(), {
        method,
        headers,
        body: opts.body !== undefined ? JSON.stringify(opts.body) : undefined,
      })
    } catch {
      throw new ApiError('network', 'network error', 0)
    }

    if (!response.ok) {
      if (response.status === 401) onUnauthorized?.()
      let envelope: ErrorEnvelope = {}
      try {
        envelope = (await response.json()) as ErrorEnvelope
      } catch {
        // non-JSON error body — fall back to the HTTP status
      }
      throw new ApiError(
        envelope.error?.code ?? `http_${response.status}`,
        envelope.error?.message ?? response.statusText,
        response.status,
        envelope.error?.field_errors ?? [],
      )
    }

    if (response.status === 204) return undefined as T
    return (await response.json()) as T
  }

  return {
    request,
    get: <T>(path: string, query?: RequestOptions['query']) => request<T>('GET', path, { query }),
    post: <T>(path: string, body?: unknown) => request<T>('POST', path, { body }),
    put: <T>(path: string, body?: unknown) => request<T>('PUT', path, { body }),
    patch: <T>(path: string, body?: unknown) => request<T>('PATCH', path, { body }),
    del: <T>(path: string) => request<T>('DELETE', path),
    /** Cursor-paginated list per C2. */
    list: <T>(path: string, params: ListParams = {}) =>
      request<Page<T>>('GET', path, { query: { cursor: params.cursor, limit: params.limit } }),
  }
}

export type ApiClient = ReturnType<typeof createApiClient>
