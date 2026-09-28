/**
 * Rate entry with unit prefixes (owner request 2026-07-19, item 5): "10G" =
 * 10 gigabit, "1M" = 1 megabit, "22" (no prefix) = 22 kbit; empty or 0 =
 * unlimited. Multipliers are binary (M = 1024k) to match the backend's
 * rateToken convention ("10M" == 10240 kbps in internal/subscribers).
 */

const RATE_RE = /^(\d+(?:\.\d+)?)\s*([kKmMgG]?)$/

/**
 * Parse a single human rate into kbps. "" and "0" mean unlimited and return
 * 0; a malformed value returns null.
 */
export function parseRateKbps(text: string): number | null {
  const s = text.trim()
  if (s === '') return 0
  const m = RATE_RE.exec(s)
  if (!m) return null
  const n = parseFloat(m[1])
  if (!isFinite(n) || n < 0) return null
  const mult = { '': 1, k: 1, m: 1024, g: 1024 * 1024 }[m[2].toLowerCase() as '' | 'k' | 'm' | 'g']
  return Math.round(n * mult)
}

/** Render kbps back to the compact entry form: 0 → "", 10240 → "10M", 22 → "22". */
export function formatRateKbps(kbps: number): string {
  if (kbps <= 0) return ''
  if (kbps % (1024 * 1024) === 0) return `${kbps / (1024 * 1024)}G`
  if (kbps % 1024 === 0) return `${kbps / 1024}M`
  return String(kbps)
}

/**
 * Parse an "up/down" (rx/tx) pair — each side using the prefix rules above —
 * into the canonical vendor-safe string stored in rate_override /
 * throttle_rate / burst fields. Every side gets an explicit suffix so a bare
 * "22" can never reach the router (RouterOS reads unsuffixed numbers as
 * bits/s). Empty input returns "" (= field unset); a malformed side returns
 * null.
 */
export function normalizeRatePair(text: string): string | null {
  const s = text.trim()
  if (s === '') return ''
  const parts = s.split('/')
  if (parts.length > 2) return null
  const sides: string[] = []
  for (const part of parts) {
    const kbps = parseRateKbps(part)
    if (kbps === null) return null
    sides.push(rateToken(kbps))
  }
  // A single value applies to both directions.
  if (sides.length === 1) sides.push(sides[0])
  return sides.join('/')
}

/** One canonical side: 0 → "0" (unlimited side), clean M multiples as "<n>M", else "<n>k". */
function rateToken(kbps: number): string {
  if (kbps <= 0) return '0'
  if (kbps % (1024 * 1024) === 0) return `${kbps / (1024 * 1024)}G`
  if (kbps % 1024 === 0) return `${kbps / 1024}M`
  return `${kbps}k`
}
