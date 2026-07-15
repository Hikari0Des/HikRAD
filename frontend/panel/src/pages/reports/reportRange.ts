/**
 * Date-range presets for the reports section (task 1, FR-45-47). The panel
 * only ever sends whole-day boundaries; Asia/Baghdad has no DST (CLAUDE.md),
 * so a fixed +03:00 offset is exact, not an approximation.
 */

const BAGHDAD_OFFSET = '+03:00'

/** Today's date in Asia/Baghdad as YYYY-MM-DD, independent of browser TZ. */
export function baghdadToday(): string {
  const parts = new Intl.DateTimeFormat('en-CA', {
    timeZone: 'Asia/Baghdad',
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
  }).formatToParts(new Date())
  const get = (t: string) => parts.find((p) => p.type === t)?.value ?? '01'
  return `${get('year')}-${get('month')}-${get('day')}`
}

// Pure calendar-date arithmetic (Date.UTC on the Y/M/D components, never a
// timezone-offset string): the +03:00 offset only matters when converting a
// date to the RFC3339 instant the API expects (toApiInstant below). Mixing it
// into setUTCDate/toISOString round-trips here previously canceled itself out
// (Baghdad midnight is the previous UTC day's 21:00, so +1 UTC day looked like
// no change once re-sliced) — caught by useReportRange.test.tsx.
function parseYMD(dateStr: string): [number, number, number] {
  const [y, m, d] = dateStr.split('-').map(Number)
  return [y, m, d]
}

function addDays(dateStr: string, days: number): string {
  const [y, m, d] = parseYMD(dateStr)
  const dt = new Date(Date.UTC(y, m - 1, d))
  dt.setUTCDate(dt.getUTCDate() + days)
  return dt.toISOString().slice(0, 10)
}

function startOfWeek(dateStr: string): string {
  const [y, m, d] = parseYMD(dateStr)
  const dow = new Date(Date.UTC(y, m - 1, d)).getUTCDay() // 0=Sun
  return addDays(dateStr, -dow)
}

function startOfMonth(dateStr: string): string {
  return dateStr.slice(0, 7) + '-01'
}

export type RangePreset = 'today' | 'week' | 'month' | 'custom'

export interface DateRange {
  /** Inclusive start date, YYYY-MM-DD (Asia/Baghdad). */
  from: string
  /** Exclusive end date, YYYY-MM-DD (Asia/Baghdad) — the day after the last included day. */
  to: string
}

/** Compute [from, to) for a preset, anchored on "today" in Asia/Baghdad. */
export function presetRange(preset: Exclude<RangePreset, 'custom'>): DateRange {
  const today = baghdadToday()
  switch (preset) {
    case 'today':
      return { from: today, to: addDays(today, 1) }
    case 'week':
      return { from: startOfWeek(today), to: addDays(today, 1) }
    case 'month':
      return { from: startOfMonth(today), to: addDays(today, 1) }
  }
}

/** Convert a date-only YYYY-MM-DD (Asia/Baghdad) into the RFC3339 instant the API expects. */
export function toApiInstant(dateStr: string): string {
  return `${dateStr}T00:00:00${BAGHDAD_OFFSET}`
}
