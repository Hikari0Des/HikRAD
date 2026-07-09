import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { I18nProvider, useLocale, useT } from './I18nProvider'
import { resolveMessage } from './messages'

describe('resolveMessage — fallback chain', () => {
  const trees = {
    en: { m: { hello: 'Hello' } },
    ar: { m: { hello: 'مرحبا' } },
    ku: { m: {} },
  }

  it('returns the locale string when present', () => {
    expect(resolveMessage(trees, 'ar', 'm.hello')).toBe('مرحبا')
  })

  it('falls back ku→en, never ku→ar, when ku lacks the key', () => {
    expect(resolveMessage(trees, 'ku', 'm.hello')).toBe('Hello')
  })

  it('returns the key itself when no locale has it', () => {
    expect(resolveMessage(trees, 'en', 'm.missing')).toBe('m.missing')
  })
})

function Probe() {
  const t = useT()
  const { locale, dir, setLocale } = useLocale()
  return (
    <div>
      <span data-testid="locale">{locale}</span>
      <span data-testid="dir">{dir}</span>
      <span data-testid="msg">{t('portal.login.submit')}</span>
      <span data-testid="interp">{t('common.quota.of', { used: '1 GB', total: '5 GB' })}</span>
      <button onClick={() => setLocale('ar')}>ar</button>
      <button onClick={() => setLocale('ku')}>ku</button>
    </div>
  )
}

describe('I18nProvider / useT / useLocale', () => {
  it('starts in en, translates, and flips dir/lang on locale switch', () => {
    render(
      <I18nProvider>
        <Probe />
      </I18nProvider>,
    )
    expect(screen.getByTestId('locale')).toHaveTextContent('en')
    expect(screen.getByTestId('dir')).toHaveTextContent('ltr')
    expect(screen.getByTestId('msg')).toHaveTextContent('Sign in')

    fireEvent.click(screen.getByText('ar'))
    expect(screen.getByTestId('dir')).toHaveTextContent('rtl')
    expect(screen.getByTestId('msg')).toHaveTextContent('تسجيل الدخول')
    expect(document.documentElement.dir).toBe('rtl')
    expect(document.documentElement.lang).toBe('ar')
    expect(window.localStorage.getItem('hikrad.locale')).toBe('ar')

    // ku is RTL like ar but its own locale.
    fireEvent.click(screen.getByText('ku'))
    expect(screen.getByTestId('dir')).toHaveTextContent('rtl')
    expect(document.documentElement.lang).toBe('ku')
  })

  it('interpolates variables through useT', () => {
    render(
      <I18nProvider>
        <Probe />
      </I18nProvider>,
    )
    expect(screen.getByTestId('interp')).toHaveTextContent('1 GB of 5 GB')
  })

  it('honors a stored locale preference on mount', () => {
    window.localStorage.setItem('hikrad.locale', 'ar')
    render(
      <I18nProvider>
        <Probe />
      </I18nProvider>,
    )
    expect(screen.getByTestId('locale')).toHaveTextContent('ar')
    expect(document.documentElement.dir).toBe('rtl')
  })
})
