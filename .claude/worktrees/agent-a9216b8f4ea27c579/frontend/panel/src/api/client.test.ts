import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { tokenStore } from '../auth/tokenStore'
import { ApiError, NetworkError, UNAUTHORIZED_EVENT, listPage, paginate, request } from './client'

const fetchMock = vi.fn()

function jsonResponse(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

beforeEach(() => {
  fetchMock.mockReset()
  vi.stubGlobal('fetch', fetchMock)
  window.localStorage.clear()
})

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('request', () => {
  it('injects the bearer token and returns parsed JSON', async () => {
    tokenStore.setTokens('access-123', 'refresh-456')
    fetchMock.mockResolvedValueOnce(jsonResponse(200, { hello: 'world' }))

    const result = await request<{ hello: string }>('/things')

    expect(result).toEqual({ hello: 'world' })
    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit]
    expect(url).toBe('/api/v1/things')
    expect((init.headers as Record<string, string>)['Authorization']).toBe('Bearer access-123')
  })

  it('parses the C2 error envelope into a typed ApiError', async () => {
    fetchMock.mockResolvedValueOnce(
      jsonResponse(422, {
        error: {
          code: 'validation',
          message: 'invalid input',
          field_errors: [{ field: 'username', message: 'required' }],
        },
      }),
    )

    const err = await request('/subscribers', { body: {} }).catch((e: unknown) => e)

    expect(err).toBeInstanceOf(ApiError)
    const apiErr = err as ApiError
    expect(apiErr.status).toBe(422)
    expect(apiErr.code).toBe('validation')
    expect(apiErr.message).toBe('invalid input')
    expect(apiErr.fieldErrors).toEqual([{ field: 'username', message: 'required' }])
  })

  it('still produces an ApiError when the body is not the envelope', async () => {
    fetchMock.mockResolvedValueOnce(new Response('boom', { status: 500 }))

    const err = await request('/things').catch((e: unknown) => e)

    expect(err).toBeInstanceOf(ApiError)
    expect((err as ApiError).status).toBe(500)
    expect((err as ApiError).code).toBe('unknown')
  })

  it('clears tokens and fires UNAUTHORIZED_EVENT on an unrecoverable 401', async () => {
    tokenStore.setTokens('stale-access', 'stale-refresh')
    fetchMock.mockResolvedValueOnce(
      jsonResponse(401, { error: { code: 'unauthorized', message: 'token expired' } }),
    )
    const unauthorized = vi.fn()
    window.addEventListener(UNAUTHORIZED_EVENT, unauthorized)

    const err = await request('/things').catch((e: unknown) => e)

    window.removeEventListener(UNAUTHORIZED_EVENT, unauthorized)
    expect(err).toBeInstanceOf(ApiError)
    expect((err as ApiError).status).toBe(401)
    expect(unauthorized).toHaveBeenCalledTimes(1)
    expect(tokenStore.getAccessToken()).toBeNull()
    expect(tokenStore.getRefreshToken()).toBeNull()
  })

  it('does not fire UNAUTHORIZED_EVENT for /auth/* failures (bad login is not a session loss)', async () => {
    fetchMock.mockResolvedValueOnce(
      jsonResponse(401, { error: { code: 'invalid_credentials', message: 'nope' } }),
    )
    const unauthorized = vi.fn()
    window.addEventListener(UNAUTHORIZED_EVENT, unauthorized)

    const err = await request('/auth/login', { body: { username: 'x', password: 'y' } }).catch(
      (e: unknown) => e,
    )

    window.removeEventListener(UNAUTHORIZED_EVENT, unauthorized)
    expect(err).toBeInstanceOf(ApiError)
    expect(unauthorized).not.toHaveBeenCalled()
  })

  it('wraps fetch failures (backend down) in NetworkError', async () => {
    fetchMock.mockRejectedValueOnce(new TypeError('fetch failed'))

    const err = await request('/things').catch((e: unknown) => e)

    expect(err).toBeInstanceOf(NetworkError)
  })
})

describe('pagination helpers', () => {
  it('listPage sends cursor and limit as query params', async () => {
    fetchMock.mockResolvedValueOnce(jsonResponse(200, { items: [], next_cursor: null }))

    await listPage('/subscribers', { cursor: 'abc', limit: 50 })

    const [url] = fetchMock.mock.calls[0] as [string]
    expect(url).toBe('/api/v1/subscribers?cursor=abc&limit=50')
  })

  it('paginate walks next_cursor until null', async () => {
    fetchMock
      .mockResolvedValueOnce(jsonResponse(200, { items: ['a', 'b'], next_cursor: 'page2' }))
      .mockResolvedValueOnce(jsonResponse(200, { items: ['c'], next_cursor: null }))

    const collected: string[] = []
    for await (const item of paginate<string>('/subscribers')) {
      collected.push(item)
    }

    expect(collected).toEqual(['a', 'b', 'c'])
    const secondUrl = (fetchMock.mock.calls[1] as [string])[0]
    expect(secondUrl).toContain('cursor=page2')
  })
})
