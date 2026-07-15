/**
 * SAS4/CSV import wizard API (contract C3, FR-6): upload -> map -> dry-run ->
 * execute -> poll. The file travels as base64 JSON (the router's frozen
 * enforceJSON middleware 415s multipart) — see backend/internal/importer/api.go.
 */
import { request } from './client'

export interface UploadResult {
  batch_id: string
  header: string[]
  encoding: string
  status: string
  column_map?: Record<string, string>
}

export function uploadImportFile(
  filename: string,
  contentBase64: string,
  preset?: string,
): Promise<UploadResult> {
  return request<UploadResult>('/import/subscribers', {
    method: 'POST',
    body: { filename, content_base64: contentBase64, preset },
  })
}

export interface MapResult {
  batch_id: string
  column_map: Record<string, string>
  status: string
}

export function mapImportColumns(
  batchId: string,
  columnMap: Record<string, string>,
  preset?: string,
): Promise<MapResult> {
  return request<MapResult>(`/import/${batchId}/map`, {
    method: 'POST',
    body: { column_map: columnMap, preset },
  })
}

export interface ImportRowReport {
  row: number
  fields: Record<string, string>
  errors: string[]
  warnings: string[]
  action: 'create' | 'skip'
}

export interface DryRunResult {
  batch_id: string
  rows: ImportRowReport[]
  total: number
  will_create: number
  will_skip: number
}

export function dryRunImport(batchId: string): Promise<DryRunResult> {
  return request<DryRunResult>(`/import/${batchId}/dry-run`, { method: 'POST' })
}

export interface ImportProgress {
  status: 'running' | 'completed'
  total: number
  done: number
  created: number
  skipped: number
  failed: number
}

export interface ExecuteResult extends ImportProgress {
  batch_id?: string
}

export function executeImport(batchId: string): Promise<ExecuteResult> {
  return request<ExecuteResult>(`/import/${batchId}/execute`, { method: 'POST' })
}

export interface ImportRowStatus extends ImportRowReport {
  status: string
  subscriber_id: string
  error: string
}

export interface BatchStatus {
  batch_id: string
  status: string
  filename: string
  encoding: string
  header: string[]
  column_map: Record<string, string>
  preset: string
  row_count: number
  summary: unknown
  progress?: ImportProgress
  rows?: ImportRowStatus[]
}

export function getImportBatch(batchId: string, withRows = false): Promise<BatchStatus> {
  return request<BatchStatus>(`/import/${batchId}`, { query: withRows ? { rows: '1' } : undefined })
}

/** Read a File as a base64 string (no data: URI prefix). */
export function fileToBase64(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader()
    reader.onload = () => {
      const result = reader.result as string
      const comma = result.indexOf(',')
      resolve(comma >= 0 ? result.slice(comma + 1) : result)
    }
    reader.onerror = () => reject(reader.error)
    reader.readAsDataURL(file)
  })
}
