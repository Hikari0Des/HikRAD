import { describe, expect, it } from 'vitest'

import type { LiveSession } from '../api/types'
import { applyLiveEvent, sessionField, type LiveMap } from './liveReducer'

function session(overrides: Partial<LiveSession> = {}): LiveSession {
  return {
    username: 'ali',
    subscriber_id: 'sub-1',
    nas_id: 'nas-1',
    acct_session_id: 'sess-1',
    ip: '10.0.0.5',
    mac: 'AA:BB:CC:DD:EE:FF',
    started_at: '2026-07-10T10:00:00Z',
    last_interim_at: '2026-07-10T10:05:00Z',
    bytes_in: 100,
    bytes_out: 50,
    rate_down_bps: 1000,
    rate_up_bps: 500,
    stale: false,
    service: 'pppoe',
    ...overrides,
  }
}

describe('applyLiveEvent (SSE reducer)', () => {
  it('replaces the whole map on a snapshot (reconnect re-sync, no ghosts)', () => {
    const start: LiveMap = new Map([['ghost:1', session({ acct_session_id: 'ghost' })]])
    const s = session()
    const next = applyLiveEvent(start, { type: 'snapshot', sessions: [s] })
    expect([...next.keys()]).toEqual([sessionField(s)])
    expect(next.get(sessionField(s))).toBe(s)
  })

  it('adds a session on upsert', () => {
    const s = session()
    const next = applyLiveEvent(new Map(), { type: 'upsert', session: s })
    expect(next.get(sessionField(s))).toBe(s)
  })

  it('flips an existing row to stale on upsert without duplicating it', () => {
    const s = session()
    let map = applyLiveEvent(new Map(), { type: 'upsert', session: s })
    map = applyLiveEvent(map, { type: 'upsert', session: session({ stale: true }) })
    expect(map.size).toBe(1)
    expect(map.get(sessionField(s))?.stale).toBe(true)
  })

  it('drops a row on remove and no-ops for unknown fields', () => {
    const s = session()
    const map = applyLiveEvent(new Map(), { type: 'upsert', session: s })
    const removed = applyLiveEvent(map, { type: 'remove', field: sessionField(s) })
    expect(removed.size).toBe(0)
    // Removing something not present returns the same reference (no churn).
    const again = applyLiveEvent(removed, { type: 'remove', field: 'nope:1' })
    expect(again).toBe(removed)
  })
})
