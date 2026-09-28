import { act, renderHook } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { describe, expect, it } from 'vitest'

import { useReportRange } from './useReportRange'

function wrapper({ children }: { children: React.ReactNode }) {
  return <MemoryRouter initialEntries={['/reports/revenue']}>{children}</MemoryRouter>
}

describe('useReportRange (task 1: URL-encoded filter state)', () => {
  it('defaults to today and updates the range when a preset is picked', () => {
    const { result } = renderHook(() => useReportRange(), { wrapper })
    expect(result.current.preset).toBe('today')
    // "today" is a single exclusive day: to is exactly one day after from.
    const from = new Date(result.current.fromDate)
    const to = new Date(result.current.toDate)
    expect((to.getTime() - from.getTime()) / 86_400_000).toBe(1)

    act(() => result.current.setPreset('month'))
    expect(result.current.preset).toBe('month')
    // "this month" always starts on day 1.
    expect(result.current.fromDate.endsWith('-01')).toBe(true)
  })

  it('switches to custom and keeps the exact dates the caller picked (shareable via URL)', () => {
    const { result } = renderHook(() => useReportRange(), { wrapper })

    act(() => result.current.setCustom('2026-01-01', '2026-01-15'))
    expect(result.current.preset).toBe('custom')
    expect(result.current.fromDate).toBe('2026-01-01')
    expect(result.current.toDate).toBe('2026-01-15')
    expect(result.current.apiFrom).toBe('2026-01-01T00:00:00+03:00')
    expect(result.current.apiTo).toBe('2026-01-15T00:00:00+03:00')
  })
})
