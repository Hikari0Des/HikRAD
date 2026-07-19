import { useEffect, useRef, useState } from 'react'

import { useAuth } from '../auth/AuthContext'

/**
 * useState persisted to localStorage per signed-in manager (item 7): a filter
 * an operator sets stays set — across navigations and sessions on this device
 * — until they clear it themselves. The stored copy is removed when the value
 * returns to its initial state, so "remove filters" really forgets it.
 *
 * Objects are shallow-merged over `initial` on load so a stored value from an
 * older shape never drops newly added fields.
 */
export function usePersistentState<T>(
  key: string,
  initial: T,
): [T, React.Dispatch<React.SetStateAction<T>>] {
  const { manager } = useAuth()
  const storageKey = `hikrad.ui.${manager?.id ?? 'anon'}.${key}`

  const [value, setValue] = useState<T>(() => load(storageKey, initial))

  // Re-read when the signed-in manager changes (login/logout without remount).
  const prevKey = useRef(storageKey)
  useEffect(() => {
    if (prevKey.current !== storageKey) {
      prevKey.current = storageKey
      setValue(load(storageKey, initial))
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [storageKey])

  useEffect(() => {
    try {
      if (JSON.stringify(value) === JSON.stringify(initial)) {
        localStorage.removeItem(storageKey)
      } else {
        localStorage.setItem(storageKey, JSON.stringify(value))
      }
    } catch {
      // Storage full/unavailable: the state still works for this session.
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [storageKey, value])

  return [value, setValue]
}

function load<T>(storageKey: string, initial: T): T {
  try {
    const raw = localStorage.getItem(storageKey)
    if (raw == null) return initial
    const parsed = JSON.parse(raw) as T
    if (
      typeof initial === 'object' &&
      initial !== null &&
      !Array.isArray(initial) &&
      typeof parsed === 'object' &&
      parsed !== null &&
      !Array.isArray(parsed)
    ) {
      return { ...initial, ...parsed }
    }
    return parsed
  } catch {
    return initial
  }
}
