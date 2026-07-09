import { describe, expect, it } from 'vitest'

import { formatDate, formatIQD, formatNumber } from './format'

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
