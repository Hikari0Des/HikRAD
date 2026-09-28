/**
 * Dark/light/system theme preference (item 19), shared by panel and portal.
 *
 * The preference persists in localStorage (`hikrad.theme`) and resolves to a
 * concrete `data-theme="light|dark"` attribute on <html>; the token files
 * (src/theme/tokens.css in each app) restyle every CSS custom property under
 * `:root[data-theme='dark']`, so components never know about themes. 'system'
 * follows the OS via prefers-color-scheme and re-resolves live when the OS
 * switches. Implemented as a tiny external store (useSyncExternalStore) so
 * any number of pickers stay in sync without a provider.
 */
import { useSyncExternalStore } from 'react'

export type ThemePreference = 'light' | 'dark' | 'system'

export const THEME_PREFERENCES: readonly ThemePreference[] = ['light', 'dark', 'system']

const THEME_STORAGE_KEY = 'hikrad.theme'

function isPreference(v: unknown): v is ThemePreference {
  return v === 'light' || v === 'dark' || v === 'system'
}

function readStored(): ThemePreference {
  try {
    const stored = window.localStorage.getItem(THEME_STORAGE_KEY)
    if (isPreference(stored)) return stored
  } catch {
    // storage unavailable (private mode etc.)
  }
  return 'system'
}

/** jsdom (tests) has no matchMedia — treat that as "light system theme". */
function systemQuery(): MediaQueryList | null {
  if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') return null
  return window.matchMedia('(prefers-color-scheme: dark)')
}

let preference: ThemePreference = typeof window === 'undefined' ? 'system' : readStored()
const listeners = new Set<() => void>()
let watchingSystem = false

function resolve(pref: ThemePreference): 'light' | 'dark' {
  if (pref !== 'system') return pref
  return systemQuery()?.matches ? 'dark' : 'light'
}

function apply(): void {
  if (typeof document === 'undefined') return
  document.documentElement.dataset.theme = resolve(preference)
}

function emit(): void {
  for (const l of listeners) l()
}

function watchSystem(): void {
  if (watchingSystem) return
  const q = systemQuery()
  if (!q) return
  watchingSystem = true
  q.addEventListener('change', () => {
    if (preference === 'system') {
      apply()
      emit()
    }
  })
}

/** Apply the stored preference to <html>. Call once at app startup. */
export function initTheme(): void {
  apply()
  watchSystem()
}

export function setThemePreference(next: ThemePreference): void {
  preference = next
  try {
    window.localStorage.setItem(THEME_STORAGE_KEY, next)
  } catch {
    // non-persistent is fine
  }
  apply()
  watchSystem()
  emit()
}

function subscribe(listener: () => void): () => void {
  listeners.add(listener)
  return () => listeners.delete(listener)
}

/** The current preference + setter, live across every consumer. */
export function useTheme(): {
  theme: ThemePreference
  setTheme: (next: ThemePreference) => void
} {
  const theme = useSyncExternalStore(
    subscribe,
    () => preference,
    () => 'system' as const,
  )
  return { theme, setTheme: setThemePreference }
}
