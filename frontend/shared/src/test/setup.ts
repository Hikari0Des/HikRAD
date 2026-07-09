import '@testing-library/jest-dom/vitest'
import { cleanup } from '@testing-library/react'
import { afterEach } from 'vitest'

// Node 22+ defines an experimental globalThis.localStorage getter that yields
// undefined unless node runs with --localstorage-file; in Vitest's jsdom
// environment window === globalThis, so it shadows jsdom's implementation.
// Replace it with an in-memory Storage for tests (mirrors the panel's setup).
class MemoryStorage implements Storage {
  private store = new Map<string, string>()
  get length() {
    return this.store.size
  }
  clear() {
    this.store.clear()
  }
  getItem(key: string) {
    return this.store.get(key) ?? null
  }
  key(index: number) {
    return [...this.store.keys()][index] ?? null
  }
  removeItem(key: string) {
    this.store.delete(key)
  }
  setItem(key: string, value: string) {
    this.store.set(key, String(value))
  }
}
if (window.localStorage === undefined) {
  Object.defineProperty(globalThis, 'localStorage', {
    value: new MemoryStorage(),
    configurable: true,
  })
}

afterEach(() => {
  cleanup()
  window.localStorage.clear()
})
