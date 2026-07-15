import { useEffect, useState } from 'react'

import { getSettingsGroup, putSettingsGroup, type SettingsGroup } from '../../api/setup'
import { ApiError, type FieldError } from '../../api/client'
import { useToast } from '../../components/Toast'

/**
 * Generic settings-group form state (FR-53): loads a group's current values,
 * tracks per-field edits and validation errors, and saves the whole group in
 * one PUT — "no partial silent saves" (edge case in the task brief): a 422
 * aborts the entire save and surfaces every field error at once.
 */
export function useSettingsGroup(group: SettingsGroup) {
  const { toast } = useToast()
  const [values, setValues] = useState<Record<string, unknown>>({})
  const [loaded, setLoaded] = useState(false)
  const [loadError, setLoadError] = useState<unknown>(undefined)
  const [errors, setErrors] = useState<Record<string, string>>({})
  const [saving, setSaving] = useState(false)

  useEffect(() => {
    let cancelled = false
    setLoaded(false)
    setLoadError(undefined)
    getSettingsGroup(group)
      .then((res) => {
        if (!cancelled) {
          setValues(res as Record<string, unknown>)
          setLoaded(true)
        }
      })
      .catch((err) => {
        if (!cancelled) {
          setLoadError(err)
          setLoaded(true)
        }
      })
    return () => {
      cancelled = true
    }
  }, [group])

  function setField(field: string, value: unknown) {
    setValues((prev) => ({ ...prev, [field]: value }))
    setErrors((prev) => {
      if (!prev[field]) return prev
      const next = { ...prev }
      delete next[field]
      return next
    })
  }

  /** `savedMessage`/`errorFallback` must already be localized by the caller. */
  async function save(
    body: Record<string, unknown>,
    savedMessage: string,
    errorFallback: string,
  ): Promise<boolean> {
    setSaving(true)
    setErrors({})
    try {
      const res = await putSettingsGroup(group, body)
      setValues(res as Record<string, unknown>)
      toast(savedMessage, 'ok')
      return true
    } catch (err) {
      if (err instanceof ApiError && err.fieldErrors.length > 0) {
        setErrors(mapErrors(err.fieldErrors))
      } else {
        toast(err instanceof Error ? err.message : errorFallback, 'danger')
      }
      return false
    } finally {
      setSaving(false)
    }
  }

  return { values, setField, errors, saving, loaded, loadError, save }
}

function mapErrors(errs: FieldError[]): Record<string, string> {
  const out: Record<string, string> = {}
  for (const e of errs) out[e.field] = e.message
  return out
}
