import { useState, type FormEvent } from 'react'
import { useNavigate } from 'react-router-dom'

import { useT } from '@hikrad/shared'

import { BRANDING } from '../branding'
import { LanguageSwitcher } from '../components/LanguageSwitcher'

/**
 * Branded login page shell (FR-43 groundwork). Branding comes from
 * placeholder tokens until server settings arrive; real subscriber
 * authentication (FR-41) lands in Phase 4 — submit currently just enters the
 * skeleton so the three routes are reachable.
 */
export function LoginPage() {
  const t = useT()
  const navigate = useNavigate()
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')

  function onSubmit(event: FormEvent) {
    event.preventDefault()
    // TODO(Phase 4): call the subscriber auth endpoint; until then the
    // skeleton is enterable so the route stubs can be reviewed.
    navigate('/home')
  }

  return (
    <div className="mx-auto flex min-h-screen w-full max-w-md flex-col justify-center gap-6 px-4 py-8">
      <div className="flex flex-col items-center gap-2 text-center">
        <span
          aria-hidden="true"
          className="flex h-14 w-14 items-center justify-center rounded-xl bg-brand text-2xl font-bold text-ink-inverse"
        >
          {BRANDING.initial}
        </span>
        <h1 className="text-xl font-semibold">{BRANDING.name}</h1>
        <p className="text-sm text-ink-muted">{t('portal.login.subtitle')}</p>
      </div>

      <form
        onSubmit={onSubmit}
        className="flex flex-col gap-4 rounded-xl bg-surface-raised p-6 shadow-sm"
      >
        <h2 className="text-base font-semibold">{t('portal.login.title')}</h2>

        <label className="flex flex-col gap-1 text-sm">
          {t('portal.login.username')}
          <input
            name="username"
            autoComplete="username"
            dir="ltr"
            value={username}
            onChange={(e) => setUsername(e.target.value)}
            className="rounded-md border border-surface-sunken bg-surface px-3 py-2 text-base"
          />
        </label>

        <label className="flex flex-col gap-1 text-sm">
          {t('portal.login.password')}
          <input
            name="password"
            type="password"
            autoComplete="current-password"
            dir="ltr"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            className="rounded-md border border-surface-sunken bg-surface px-3 py-2 text-base"
          />
        </label>

        <button
          type="submit"
          className="rounded-md bg-brand py-2 font-semibold text-ink-inverse transition-colors hover:bg-brand-strong"
        >
          {t('portal.login.submit')}
        </button>

        <p className="text-xs text-ink-muted">{t('portal.login.note')}</p>
      </form>

      <div className="flex justify-center">
        <LanguageSwitcher />
      </div>
    </div>
  )
}
