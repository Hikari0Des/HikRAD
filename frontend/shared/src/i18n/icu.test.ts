import { describe, expect, it } from 'vitest'

import { formatMessage } from './icu'

const FSI = '⁨'
const PDI = '⁩'

describe('formatMessage — interpolation', () => {
  it('substitutes {name} variables verbatim', () => {
    expect(formatMessage('Hello {name}', { name: 'Sara' }, 'en')).toBe('Hello Sara')
  })

  it('substitutes several variables in one message', () => {
    expect(
      formatMessage('Subscriber {username} used {gb} GB', { username: 'ali99', gb: '4.2' }, 'en'),
    ).toBe('Subscriber ali99 used 4.2 GB')
  })

  it('leaves missing variables visible as {name}', () => {
    expect(formatMessage('Hello {name}', {}, 'en')).toBe('Hello {name}')
    expect(formatMessage('Hello {name}', undefined, 'en')).toBe('Hello {name}')
  })

  it('returns plain messages untouched', () => {
    expect(formatMessage('No placeholders here', undefined, 'en')).toBe('No placeholders here')
  })

  it('bidi-isolates interpolated values when opts.isolate is set', () => {
    const out = formatMessage(
      'استخدم المشترك {username} ما مقداره {gb} غيغابايت',
      { username: 'ali99', gb: '4.2' },
      'ar',
      { isolate: true },
    )
    expect(out).toContain(`${FSI}ali99${PDI}`)
    expect(out).toContain(`${FSI}4.2${PDI}`)
  })
})

describe('formatMessage — plurals', () => {
  const msg = '{count, plural, =0 {no days} one {# day} other {# days}}'

  it('selects the exact-match branch', () => {
    expect(formatMessage(msg, { count: 0 }, 'en')).toBe('no days')
  })

  it('selects one/other via Intl.PluralRules and formats #', () => {
    expect(formatMessage(msg, { count: 1 }, 'en')).toBe('1 day')
    expect(formatMessage(msg, { count: 5 }, 'en')).toBe('5 days')
  })

  it('supports the richer Arabic plural categories', () => {
    const ar =
      '{count, plural, zero {صفر} one {واحد} two {اثنان} few {قليل} many {كثير} other {غير ذلك}}'
    expect(formatMessage(ar, { count: 1 }, 'ar')).toBe('واحد')
    expect(formatMessage(ar, { count: 2 }, 'ar')).toBe('اثنان')
    expect(formatMessage(ar, { count: 3 }, 'ar')).toBe('قليل')
    expect(formatMessage(ar, { count: 11 }, 'ar')).toBe('كثير')
  })

  it('falls back to the other branch when a category branch is absent', () => {
    expect(formatMessage('{n, plural, other {# items}}', { n: 1 }, 'en')).toBe('1 items')
  })

  it('formats # with Eastern Arabic digits when numerals are forced', () => {
    const out = formatMessage('{count, plural, other {#}}', { count: 42 }, 'ar', {
      numerals: 'arab',
    })
    expect(out).toMatch(/^[٠-٩]+$/)
  })

  it('supports interpolation inside plural branches', () => {
    const out = formatMessage(
      '{count, plural, other {{name} has # items}}',
      { count: 3, name: 'Omar' },
      'en',
    )
    expect(out).toBe('Omar has 3 items')
  })
})

describe('formatMessage — select', () => {
  it('selects by value with other fallback', () => {
    const msg = '{status, select, active {Active now} other {Unknown}}'
    expect(formatMessage(msg, { status: 'active' }, 'en')).toBe('Active now')
    expect(formatMessage(msg, { status: 'weird' }, 'en')).toBe('Unknown')
  })
})
