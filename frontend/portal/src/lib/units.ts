/**
 * Byte + rate formatting helpers (mirrors frontend/panel/src/lib/units.ts).
 * Unit symbols (KB/MB/GB, Kbps/Mbps) are universal latin abbreviations kept
 * LTR; only the numeric part is localized by the caller's `formatNumber`.
 */

const BYTE_UNITS = ['B', 'KB', 'MB', 'GB', 'TB', 'PB']

export function formatBytes(
  bytes: number,
  formatNumber: (v: number, opts?: Intl.NumberFormatOptions) => string,
): string {
  if (!Number.isFinite(bytes) || bytes <= 0) return `${formatNumber(0)} B`
  const exp = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), BYTE_UNITS.length - 1)
  const value = bytes / Math.pow(1024, exp)
  const digits = exp === 0 ? 0 : value < 10 ? 2 : value < 100 ? 1 : 0
  return `${formatNumber(value, { maximumFractionDigits: digits })} ${BYTE_UNITS[exp]}`
}

/** Format a profile rate given in kbps as a compact Kbps/Mbps token. */
export function formatKbps(
  kbps: number,
  formatNumber: (v: number, opts?: Intl.NumberFormatOptions) => string,
): string {
  if (kbps <= 0) return `${formatNumber(0)} Kbps`
  if (kbps % 1024 === 0) return `${formatNumber(kbps / 1024)} Mbps`
  return `${formatNumber(kbps)} Kbps`
}
