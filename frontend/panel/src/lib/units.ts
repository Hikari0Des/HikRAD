/**
 * Byte + rate formatting helpers. Unit symbols (KB/MB/GB, Kbps/Mbps) are
 * universal latin abbreviations kept LTR; only the numeric part is localized by
 * the caller's `formatNumber` (locale digits). Because these build strings in a
 * function (not JSX literals), the i18n gate does not treat the symbols as
 * hardcoded UI copy.
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

/** Format a bits/second rate as Kbps/Mbps (the interim average from C6). */
export function formatBps(
  bps: number,
  formatNumber: (v: number, opts?: Intl.NumberFormatOptions) => string,
): string {
  if (!Number.isFinite(bps) || bps <= 0) return `${formatNumber(0)} Kbps`
  if (bps >= 1_000_000) {
    return `${formatNumber(bps / 1_000_000, { maximumFractionDigits: 1 })} Mbps`
  }
  return `${formatNumber(Math.round(bps / 1000))} Kbps`
}

/** Render kbps as a compact rate token, e.g. 10240 → "10 Mbps", 512 → "512 Kbps". */
export function formatKbps(
  kbps: number,
  formatNumber: (v: number, opts?: Intl.NumberFormatOptions) => string,
): string {
  if (kbps <= 0) return `${formatNumber(0)} Kbps`
  if (kbps % 1024 === 0) return `${formatNumber(kbps / 1024)} Mbps`
  return `${formatNumber(kbps)} Kbps`
}
