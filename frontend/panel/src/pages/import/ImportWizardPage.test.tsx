import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi, beforeEach } from 'vitest'

import { I18nProvider } from '@hikrad/shared'
import en from '@hikrad/shared/locales/en/panel.json'

import { ApiError } from '../../api/client'
import { ToastProvider } from '../../components/Toast'
import { ImportWizardPage } from './ImportWizardPage'

const uploadImportFile = vi.fn()
const mapImportColumns = vi.fn()
const dryRunImport = vi.fn()
const executeImport = vi.fn()
const getImportBatch = vi.fn()

vi.mock('../../api/importer', async () => {
  const actual = await vi.importActual<typeof import('../../api/importer')>('../../api/importer')
  return {
    ...actual,
    uploadImportFile: (...args: unknown[]) => uploadImportFile(...args),
    mapImportColumns: (...args: unknown[]) => mapImportColumns(...args),
    dryRunImport: (...args: unknown[]) => dryRunImport(...args),
    executeImport: (...args: unknown[]) => executeImport(...args),
    getImportBatch: (...args: unknown[]) => getImportBatch(...args),
    fileToBase64: () => Promise.resolve('base64content'),
  }
})

function renderPage() {
  return render(
    <I18nProvider>
      <ToastProvider>
        <ImportWizardPage />
      </ToastProvider>
    </I18nProvider>,
  )
}

function makeFile() {
  return new File(['username\nali'], 'subs.csv', { type: 'text/csv' })
}

beforeEach(() => {
  vi.clearAllMocks()
})

describe('ImportWizardPage state machine (task 3, FR-6)', () => {
  it('never dead-ends: an upload failure shows the reason and the picker stays usable', async () => {
    const user = userEvent.setup()
    uploadImportFile.mockRejectedValue(
      new ApiError(422, 'validation_failed', 'request validation failed', [
        { field: 'content_base64', message: 'could not read a header row' },
      ]),
    )
    renderPage()

    const input = document.querySelector('input[type="file"]') as HTMLInputElement
    await user.upload(input, makeFile())

    expect(await screen.findByText('could not read a header row')).toBeInTheDocument()
    // The file input is still present and enabled — the user can try again.
    expect(document.querySelector('input[type="file"]')).not.toBeDisabled()
  })

  it('walks upload → map → dry-run → execute → summary', async () => {
    const user = userEvent.setup()
    uploadImportFile.mockResolvedValue({
      batch_id: 'b1',
      header: ['username', 'name'],
      encoding: 'utf-8',
      status: 'uploaded',
    })
    mapImportColumns.mockResolvedValue({
      batch_id: 'b1',
      column_map: { username: 'username' },
      status: 'mapped',
    })
    dryRunImport.mockResolvedValue({
      batch_id: 'b1',
      rows: [{ row: 1, fields: {}, errors: [], warnings: [], action: 'create' }],
      total: 1,
      will_create: 1,
      will_skip: 0,
    })
    executeImport.mockResolvedValue({
      status: 'running',
      total: 1,
      done: 0,
      created: 0,
      skipped: 0,
      failed: 0,
    })
    getImportBatch.mockResolvedValue({
      batch_id: 'b1',
      status: 'completed',
      filename: 'subs.csv',
      encoding: 'utf-8',
      header: ['username'],
      column_map: { username: 'username' },
      preset: '',
      row_count: 1,
      summary: null,
      progress: { status: 'completed', total: 1, done: 1, created: 1, skipped: 0, failed: 0 },
    })

    renderPage()

    const input = document.querySelector('input[type="file"]') as HTMLInputElement
    await user.upload(input, makeFile())

    // Map step: apply the SAS4 preset to map "username", then run the dry-run.
    await user.click(await screen.findByRole('button', { name: en.import.map.sas4Preset }))
    await user.click(screen.getByRole('button', { name: en.import.map.dryRun }))

    // Dry-run step.
    await screen.findByText(en.import.execute)
    await user.click(screen.getByRole('button', { name: en.import.execute }))

    // Summary step, polled via getImportBatch.
    await waitFor(() => expect(executeImport).toHaveBeenCalledWith('b1'))
    expect(await screen.findByText(en.import.newImport)).toBeInTheDocument()
  })
})
