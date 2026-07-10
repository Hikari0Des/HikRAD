import { useEffect, useMemo, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'

import { Ltr, StatusBadge, useT } from '@hikrad/shared'

import { search } from '../api/subscribers'
import type { SearchHit } from '../api/types'
import { useDebouncedValue } from '../hooks/useDebouncedValue'

function isEditable(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) return false
  return (
    target instanceof HTMLInputElement ||
    target instanceof HTMLTextAreaElement ||
    target.isContentEditable
  )
}

/**
 * Global instant search (FR-2). Keyboard-first: '/' focuses it from anywhere,
 * ↑/↓ move the highlight, Enter opens the top (or highlighted) hit, Esc closes.
 * The query is debounced and each request aborts the previous. Results are
 * grouped by type (only subscribers this phase) and the input stays LTR-neutral
 * so Arabic names and latin usernames both read correctly.
 */
export function GlobalSearch() {
  const t = useT()
  const navigate = useNavigate()
  const inputRef = useRef<HTMLInputElement>(null)
  const boxRef = useRef<HTMLDivElement>(null)

  const [query, setQuery] = useState('')
  const [open, setOpen] = useState(false)
  const [hits, setHits] = useState<SearchHit[]>([])
  const [loading, setLoading] = useState(false)
  const [active, setActive] = useState(0)
  const debounced = useDebouncedValue(query.trim(), 250)

  // '/' focuses search from anywhere outside an editable element.
  useEffect(() => {
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key === '/' && !e.ctrlKey && !e.metaKey && !e.altKey && !isEditable(e.target)) {
        e.preventDefault()
        inputRef.current?.focus()
      }
    }
    window.addEventListener('keydown', onKeyDown)
    return () => window.removeEventListener('keydown', onKeyDown)
  }, [])

  // Debounced fetch with abort of the in-flight request.
  useEffect(() => {
    if (debounced === '') {
      setHits([])
      setLoading(false)
      return
    }
    const controller = new AbortController()
    setLoading(true)
    search(debounced, controller.signal)
      .then((res) => {
        setHits(res.items)
        setActive(0)
        setLoading(false)
      })
      .catch((err) => {
        if (err instanceof DOMException && err.name === 'AbortError') return
        setHits([])
        setLoading(false)
      })
    return () => controller.abort()
  }, [debounced])

  // Close on outside click.
  useEffect(() => {
    const onClick = (e: MouseEvent) => {
      if (boxRef.current && !boxRef.current.contains(e.target as Node)) setOpen(false)
    }
    document.addEventListener('mousedown', onClick)
    return () => document.removeEventListener('mousedown', onClick)
  }, [])

  const openHit = (hit: SearchHit | undefined) => {
    if (!hit) return
    setOpen(false)
    setQuery('')
    navigate(`/subscribers/${hit.id}`)
  }

  const onKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'ArrowDown') {
      e.preventDefault()
      setActive((a) => Math.min(a + 1, hits.length - 1))
    } else if (e.key === 'ArrowUp') {
      e.preventDefault()
      setActive((a) => Math.max(a - 1, 0))
    } else if (e.key === 'Enter') {
      e.preventDefault()
      openHit(hits[active])
    } else if (e.key === 'Escape') {
      setOpen(false)
    }
  }

  const showDropdown = open && query.trim() !== ''
  const skeletons = useMemo(() => [0, 1, 2], [])

  return (
    <div ref={boxRef} className="relative min-w-0 max-w-md flex-1">
      <input
        ref={inputRef}
        type="search"
        aria-label={t('search.label')}
        placeholder={t('search.placeholder')}
        value={query}
        onChange={(e) => {
          setQuery(e.target.value)
          setOpen(true)
        }}
        onFocus={() => setOpen(true)}
        onKeyDown={onKeyDown}
        role="combobox"
        aria-expanded={showDropdown}
        aria-controls="global-search-results"
        className="w-full rounded-md border border-surface-sunken bg-surface px-3 py-1.5 text-sm placeholder:text-ink-muted focus:border-brand focus:outline-none"
      />
      {showDropdown ? (
        <div
          id="global-search-results"
          role="listbox"
          className="absolute inset-x-0 top-full z-30 mt-1 max-h-80 overflow-y-auto rounded-md border border-surface-sunken bg-surface-raised py-1 shadow-lg"
        >
          {loading && hits.length === 0 ? (
            skeletons.map((i) => (
              <div key={i} className="px-3 py-2">
                <div className="h-3 w-32 animate-pulse rounded bg-surface-sunken" />
                <div className="mt-1.5 h-2.5 w-20 animate-pulse rounded bg-surface-sunken" />
              </div>
            ))
          ) : hits.length === 0 ? (
            <p className="px-3 py-4 text-center text-sm text-ink-muted">{t('search.noResults')}</p>
          ) : (
            <>
              <p className="px-3 pb-1 pt-1.5 text-xs font-medium uppercase tracking-wide text-ink-muted">
                {t('search.groupSubscribers')}
              </p>
              {hits.map((hit, i) => (
                <button
                  key={hit.id}
                  type="button"
                  role="option"
                  aria-selected={i === active}
                  onMouseEnter={() => setActive(i)}
                  onClick={() => openHit(hit)}
                  className={`flex w-full items-center justify-between gap-3 px-3 py-2 text-start text-sm ${
                    i === active ? 'bg-brand-soft' : 'hover:bg-surface-sunken'
                  }`}
                >
                  <span className="min-w-0">
                    <Ltr className="block truncate font-medium">{hit.username}</Ltr>
                    {hit.name ? (
                      <span className="block truncate text-xs text-ink-muted">{hit.name}</span>
                    ) : null}
                  </span>
                  <StatusBadge status={hit.status} />
                </button>
              ))}
            </>
          )}
        </div>
      ) : null}
    </div>
  )
}
