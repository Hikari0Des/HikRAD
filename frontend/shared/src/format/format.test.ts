import { describe, expect, it } from 'vitest'

import { formatDate, formatIQD, formatMoney, formatNumber } from './format'

const EASTERN = /[٠-٩]/
const WESTERN = /[0-9]/

describe('formatIQD', () => {
  it('formats IQD with no minor unit in en', () => {
    const out = formatIQD(15000, 'en')
    expect(out).toContain('IQD')
    expect(out).toMatch(/15,000/)
  })

  it('uses Eastern Arabic numerals when forced (NFR-6.3)', () => {
    const out = formatIQD(15000, 'ar', { numerals: 'arab' })
    expect(out).toMatch(EASTERN)
    expect(out).not.toMatch(WESTERN)
  })

  it('uses Western numerals when forced to latn', () => {
    const out = formatIQD(15000, 'ar', { numerals: 'latn' })
    expect(out).toMatch(WESTERN)
    expect(out).not.toMatch(EASTERN)
  })

  it('defaults to en', () => {
    expect(formatIQD(500)).toContain('IQD')
  })
})

// AC-70a regression lock (v2 phase 4, contract C8): formatMoney(amount, 'IQD', ...)
// must be byte-identical to formatIQD's output for every case above.
describe('formatMoney (AC-70a regression lock + multi-currency)', () => {
  it('is byte-identical to formatIQD for IQD, every existing case', () => {
    expect(formatMoney(15000, 'IQD', 'en')).toBe(formatIQD(15000, 'en'))
    expect(formatMoney(15000, 'IQD', 'ar', { numerals: 'arab' })).toBe(
      formatIQD(15000, 'ar', { numerals: 'arab' }),
    )
    expect(formatMoney(15000, 'IQD', 'ar', { numerals: 'latn' })).toBe(
      formatIQD(15000, 'ar', { numerals: 'latn' }),
    )
    expect(formatMoney(500, 'IQD')).toBe(formatIQD(500))
  })

  it('formats USD with 2 minor-unit digits in en', () => {
    const out = formatMoney(150.5, 'USD', 'en')
    expect(out).toContain('$')
    expect(out).toMatch(/150\.50/)
  })

  it('formats EUR with 2 minor-unit digits in en', () => {
    const out = formatMoney(99.9, 'EUR', 'en')
    expect(out).toMatch(/99\.90/)
  })

  it('uses Eastern Arabic numerals for USD when forced (NFR-6.3)', () => {
    const out = formatMoney(150.5, 'USD', 'ar', { numerals: 'arab' })
    expect(out).toMatch(EASTERN)
    expect(out).not.toMatch(WESTERN)
  })
})

describe('formatNumber', () => {
  it('formats with locale digits', () => {
    expect(formatNumber(4.2, 'en')).toBe('4.2')
    expect(formatNumber(4.2, 'ar', { numerals: 'arab' })).toMatch(EASTERN)
  })

  it('passes through Intl options (percent)', () => {
    expect(formatNumber(0.42, 'en', { style: 'percent' })).toBe('42%')
  })
})

describe('formatDate', () => {
  it('renders in the Asia/Baghdad timezone (UTC+3)', () => {
    // Midnight UTC = 03:00 in Baghdad.
    const out = formatDate('2026-07-08T00:00:00Z', 'en')
    expect(out).toContain('2026')
    expect(out).toMatch(/3:00/)
  })

  it('accepts Date objects and honors forced numerals', () => {
    const out = formatDate(new Date('2026-07-08T00:00:00Z'), 'ar', { numerals: 'arab' })
    expect(out).toMatch(EASTERN)
  })
})
