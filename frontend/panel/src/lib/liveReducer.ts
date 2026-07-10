/**
 * Pure reducer for the Live Sessions feed (contract C6). Kept dependency-free
 * and side-effect-free so it can be unit-tested without the DOM or a network:
 * `snapshot` replaces the whole set (the reconnect re-sync — no ghost rows),
 * `upsert` adds/updates one row (including flipping a row to stale), `remove`
 * drops one by its `nasID:acctSessionID` field key.
 */
import type { LiveEvent } from '../api/live'
import type { LiveSession } from '../api/types'

/** The field key hikrad-acct uses for a live session (livestate.Field). */
export function sessionField(s: Pick<LiveSession, 'nas_id' | 'acct_session_id'>): string {
  return `${s.nas_id}:${s.acct_session_id}`
}

export type LiveMap = Map<string, LiveSession>

export function applyLiveEvent(state: LiveMap, evt: LiveEvent): LiveMap {
  switch (evt.type) {
    case 'snapshot': {
      const next: LiveMap = new Map()
      for (const s of evt.sessions) next.set(sessionField(s), s)
      return next
    }
    case 'upsert': {
      const next = new Map(state)
      next.set(sessionField(evt.session), evt.session)
      return next
    }
    case 'remove': {
      if (!state.has(evt.field)) return state
      const next = new Map(state)
      next.delete(evt.field)
      return next
    }
    default:
      return state
  }
}
