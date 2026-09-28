import { useSearchParams } from 'react-router-dom'

import { presetRange, toApiInstant, type RangePreset } from './reportRange'

export interface ReportRangeState {
  preset: RangePreset
  /** Date-only (YYYY-MM-DD), always populated for the date inputs. */
  fromDate: string
  toDate: string
  /** RFC3339 instants for the API call. */
  apiFrom: string
  apiTo: string
  setPreset: (p: Exclude<RangePreset, 'custom'>) => void
  setCustom: (from: string, to: string) => void
}

/**
 * Shareable, URL-encoded date-range state (task 1: "filter bars with
 * URL-encoded state"). Reads/writes `?range=today|week|month|custom&from&to`.
 */
export function useReportRange(): ReportRangeState {
  const [params, setParams] = useSearchParams()
  const preset = (params.get('range') as RangePreset) || 'today'
  const customFrom = params.get('from') ?? ''
  const customTo = params.get('to') ?? ''

  let fromDate: string
  let toDate: string
  if (preset === 'custom' && customFrom && customTo) {
    fromDate = customFrom
    toDate = customTo
  } else {
    const r = presetRange(preset === 'custom' ? 'today' : preset)
    fromDate = r.from
    toDate = r.to
  }

  function setPreset(p: Exclude<RangePreset, 'custom'>) {
    const next = new URLSearchParams(params)
    next.set('range', p)
    next.delete('from')
    next.delete('to')
    setParams(next, { replace: true })
  }

  function setCustom(from: string, to: string) {
    const next = new URLSearchParams(params)
    next.set('range', 'custom')
    next.set('from', from)
    next.set('to', to)
    setParams(next, { replace: true })
  }

  return {
    preset,
    fromDate,
    toDate,
    apiFrom: toApiInstant(fromDate),
    // toDate is the exclusive upper bound already (day after the last included day).
    apiTo: toApiInstant(toDate),
    setPreset,
    setCustom,
  }
}
