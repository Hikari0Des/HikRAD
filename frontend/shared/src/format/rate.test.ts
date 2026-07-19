import { describe, expect, it } from 'vitest'

import { formatRateKbps, normalizeRatePair, parseRateKbps } from './rate'

describe('parseRateKbps', () => {
  it('empty and 0 mean unlimited (0)', () => {
    expect(parseRateKbps('')).toBe(0)
    expect(parseRateKbps('  ')).toBe(0)
    expect(parseRateKbps('0')).toBe(0)
  })
  it('bare numbers are kbit', () => {
    expect(parseRateKbps('22')).toBe(22)
    expect(parseRateKbps('512')).toBe(512)
  })
  it('prefixes scale binary (matches backend rateToken)', () => {
    expect(parseRateKbps('1M')).toBe(1024)
    expect(parseRateKbps('10m')).toBe(10240)
    expect(parseRateKbps('10G')).toBe(10 * 1024 * 1024)
    expect(parseRateKbps('64k')).toBe(64)
    expect(parseRateKbps('1.5M')).toBe(1536)
  })
  it('rejects malformed values', () => {
    expect(parseRateKbps('abc')).toBeNull()
    expect(parseRateKbps('10MB')).toBeNull()
    expect(parseRateKbps('-5')).toBeNull()
    expect(parseRateKbps('10 20')).toBeNull()
  })
})

describe('formatRateKbps', () => {
  it('round-trips compactly', () => {
    expect(formatRateKbps(0)).toBe('')
    expect(formatRateKbps(10240)).toBe('10M')
    expect(formatRateKbps(10 * 1024 * 1024)).toBe('10G')
    expect(formatRateKbps(22)).toBe('22')
  })
})

describe('normalizeRatePair', () => {
  it('empty stays empty (field unset)', () => {
    expect(normalizeRatePair('')).toBe('')
  })
  it('always emits explicit suffixes so RouterOS cannot misread kbit', () => {
    expect(normalizeRatePair('2M/10M')).toBe('2M/10M')
    expect(normalizeRatePair('22/500')).toBe('22k/500k')
    expect(normalizeRatePair('1024/2048')).toBe('1M/2M')
  })
  it('single value applies to both directions', () => {
    expect(normalizeRatePair('5M')).toBe('5M/5M')
  })
  it('rejects malformed pairs', () => {
    expect(normalizeRatePair('a/b')).toBeNull()
    expect(normalizeRatePair('1M/2M/3M')).toBeNull()
  })
})
