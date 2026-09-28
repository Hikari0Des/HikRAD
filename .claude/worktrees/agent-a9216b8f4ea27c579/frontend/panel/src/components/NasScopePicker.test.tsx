import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { I18nProvider } from '@hikrad/shared'
import en from '@hikrad/shared/locales/en/panel.json'

import type { NasScope } from '../api/types'
import { NasScopePicker, toggleScope } from './NasScopePicker'

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
    {
      id: 'nas-b',
      name: 'Mansour tower',
      ip: '10.0.0.2',
      services: [],
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

function renderPicker(props: { scopes: NasScope[]; onChange?: (n: NasScope[]) => void }) {
  return render(
    <I18nProvider>
      <NasScopePicker scopes={props.scopes} onChange={props.onChange ?? (() => {})} />
    </I18nProvider>,
  )
}

async function openMenu() {
  fireEvent.click(await screen.findByText(en.nasScope.add))
}

// The most important thing this component can say. An empty selection is "any
// NAS" (v1's behaviour, and what nearly every account has) — the opposite of
// "nowhere", which is how an unlabelled empty list would read.
it('says an empty selection means any NAS', async () => {
  renderPicker({ scopes: [] })
  await waitFor(() => expect(screen.getByText(en.nasScope.anyNas)).toBeInTheDocument())
})

it('offers every NAS and its service instances, named by label and kind', async () => {
  renderPicker({ scopes: [] })
  await openMenu()
  expect(screen.getByText('Karrada tower — every service')).toBeInTheDocument()
  expect(screen.getByText('Mansour tower — every service')).toBeInTheDocument()
  expect(screen.getByText('Subscribers (PPPoE)')).toBeInTheDocument()
  expect(screen.getByText('Lobby (Hotspot)')).toBeInTheDocument()
})

// The whole point of the multi-select: two NASes at once, which the single
// dropdown this replaced could not express.
it('selects several NASes', async () => {
  const onChange = vi.fn()
  renderPicker({ scopes: [{ nas_id: 'nas-a', nas_service_id: '' }], onChange })
  await openMenu()
  fireEvent.click(screen.getByText('Mansour tower — every service'))
  expect(onChange).toHaveBeenCalledWith([
    { nas_id: 'nas-a', nas_service_id: '' },
    { nas_id: 'nas-b', nas_service_id: '' },
  ])
})

it('shows each selected scope as a removable chip', async () => {
  const onChange = vi.fn()
  renderPicker({ scopes: [{ nas_id: 'nas-a', nas_service_id: 'svc-lobby' }], onChange })
  await waitFor(() => expect(screen.getByText('Lobby (Hotspot)')).toBeInTheDocument())
  fireEvent.click(screen.getByLabelText('Remove Lobby (Hotspot)'))
  expect(onChange).toHaveBeenCalledWith([])
})

describe('toggleScope', () => {
  it('adds and removes', () => {
    const a = { nas_id: 'nas-a', nas_service_id: '' }
    expect(toggleScope([], a)).toEqual([a])
    expect(toggleScope([a], a)).toEqual([])
  })

  // Selecting the whole NAS supersedes its per-service picks: the NAS-wide entry
  // already allows them, and leaving both would show a contradictory list.
  it('choosing a whole NAS drops that NAS’s service scopes', () => {
    const scopes = [
      { nas_id: 'nas-a', nas_service_id: 'svc-lobby' },
      { nas_id: 'nas-b', nas_service_id: 'svc-x' },
    ]
    expect(toggleScope(scopes, { nas_id: 'nas-a', nas_service_id: '' })).toEqual([
      { nas_id: 'nas-b', nas_service_id: 'svc-x' },
      { nas_id: 'nas-a', nas_service_id: '' },
    ])
  })

  // ...and the reverse narrows, rather than doing nothing. A checkbox that
  // visibly does nothing when clicked is worse than one that is disabled.
  it('choosing a service on a whole-NAS selection narrows to that service', () => {
    const scopes = [{ nas_id: 'nas-a', nas_service_id: '' }]
    expect(toggleScope(scopes, { nas_id: 'nas-a', nas_service_id: 'svc-lobby' })).toEqual([
      { nas_id: 'nas-a', nas_service_id: 'svc-lobby' },
    ])
  })

  // A whole-NAS pick must not swallow another NAS's service scope — that would
  // silently widen the other NAS from one zone to all of them.
  it('does not touch another NAS’s scopes', () => {
    const scopes = [{ nas_id: 'nas-b', nas_service_id: 'svc-x' }]
    expect(toggleScope(scopes, { nas_id: 'nas-a', nas_service_id: '' })).toEqual([
      { nas_id: 'nas-b', nas_service_id: 'svc-x' },
      { nas_id: 'nas-a', nas_service_id: '' },
    ])
  })
})
