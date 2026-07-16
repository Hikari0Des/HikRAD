import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { I18nProvider } from '@hikrad/shared'
import en from '@hikrad/shared/locales/en/panel.json'

import { NasScopePicker } from './NasScopePicker'

const fetchMock = vi.fn()

const NAS_LIST = {
  items: [
    {
      id: 'nas-a',
      name: 'Karrada tower',
      ip: '10.0.0.1',
      services: [
        {
          id: 'svc-ppp',
          service: 'pppoe',
          label: 'Subscribers',
          interface_note: '',
          ip_pool_id: null,
          ip_pool_name: '',
          ros_server_name: '',
          enabled: true,
          live_sessions: 3,
        },
        {
          id: 'svc-lobby',
          service: 'hotspot',
          label: 'Lobby',
          interface_note: '',
          ip_pool_id: null,
          ip_pool_name: '',
          ros_server_name: 'lobby',
          enabled: true,
          live_sessions: 1,
        },
      ],
      vendor: 'mikrotik',
      coa_port: 3799,
      has_snmp: false,
      ros_version: '7',
      location: '',
      enabled: true,
      api_port: 8728,
      api_user: '',
      has_api_creds: false,
      created_at: '2026-07-01T00:00:00Z',
      updated_at: '2026-07-01T00:00:00Z',
    },
  ],
}

beforeEach(() => {
  fetchMock.mockReset()
  fetchMock.mockResolvedValue(
    new Response(JSON.stringify(NAS_LIST), {
      status: 200,
      headers: { 'Content-Type': 'application/json' },
    }),
  )
  vi.stubGlobal('fetch', fetchMock)
})

afterEach(() => vi.unstubAllGlobals())

function renderPicker(props: {
  nasId: string
  nasServiceId: string
  onChange: (n: { nasId: string; nasServiceId: string }) => void
}) {
  return render(
    <I18nProvider>
      <NasScopePicker {...props} />
    </I18nProvider>,
  )
}

it('defaults to "any NAS" — the v1 behaviour an unscoped account keeps', async () => {
  renderPicker({ nasId: '', nasServiceId: '', onChange: () => {} })
  await waitFor(() => expect(screen.getByText('Karrada tower')).toBeInTheDocument())
  const [nasSel] = screen.getAllByRole('combobox')
  expect((nasSel as HTMLSelectElement).value).toBe('')
  expect(screen.getByText(en.nasScope.anyNas)).toBeInTheDocument()
})

// The service select is meaningless without a NAS, and the backend never stores
// that pair — so it stays disabled until one is chosen.
it('disables the service select until a NAS is picked', async () => {
  renderPicker({ nasId: '', nasServiceId: '', onChange: () => {} })
  await waitFor(() => expect(screen.getByText('Karrada tower')).toBeInTheDocument())
  const [, svcSel] = screen.getAllByRole('combobox')
  expect(svcSel).toBeDisabled()
})

it("lists the chosen NAS's service instances by label and kind", async () => {
  renderPicker({ nasId: 'nas-a', nasServiceId: '', onChange: () => {} })
  await waitFor(() => expect(screen.getByText('Subscribers (PPPoE)')).toBeInTheDocument())
  expect(screen.getByText('Lobby (Hotspot)')).toBeInTheDocument()
})

// The invariant the AuthView loader depends on: nas_service_id is never set
// while nas_id is empty, because the loader reads the pair as a whole keyed on
// nas_id and would silently ignore an orphaned service scope.
it('clears the service when the NAS is cleared', async () => {
  const onChange = vi.fn()
  renderPicker({ nasId: 'nas-a', nasServiceId: 'svc-lobby', onChange })
  await waitFor(() => expect(screen.getByText('Lobby (Hotspot)')).toBeInTheDocument())

  const [nasSel] = screen.getAllByRole('combobox')
  fireEvent.change(nasSel, { target: { value: '' } })
  expect(onChange).toHaveBeenCalledWith({ nasId: '', nasServiceId: '' })
})

// Same reason, the other direction: switching NAS must not keep the old NAS's
// service id, which would be an unsatisfiable scope (rejects every login).
it('clears the service when the NAS changes', async () => {
  const onChange = vi.fn()
  renderPicker({ nasId: '', nasServiceId: '', onChange })
  await waitFor(() => expect(screen.getByText('Karrada tower')).toBeInTheDocument())

  const [nasSel] = screen.getAllByRole('combobox')
  fireEvent.change(nasSel, { target: { value: 'nas-a' } })
  expect(onChange).toHaveBeenCalledWith({ nasId: 'nas-a', nasServiceId: '' })
})
