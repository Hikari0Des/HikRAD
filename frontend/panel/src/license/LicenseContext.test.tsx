import { render, screen, waitFor } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { LicenseProvider, useLicense } from './LicenseContext'

const getLicense = vi.fn()
vi.mock('../api/setup', () => ({
  getLicense: (...args: unknown[]) => getLicense(...args),
}))

function Probe() {
  const { license, isReadOnly } = useLicense()
  if (!license) return <span>loading</span>
  return <span>{isReadOnly ? 'read-only' : 'read-write'}</span>
}

describe('LicenseContext (task 5: read-only gating)', () => {
  it('is not read-only for a valid license', async () => {
    getLicense.mockResolvedValue({ installed: true, state: 'valid', fingerprint: 'fp' })
    render(
      <LicenseProvider>
        <Probe />
      </LicenseProvider>,
    )
    expect(await screen.findByText('read-write')).toBeInTheDocument()
  })

  it('is not read-only during grace (mutations still allowed)', async () => {
    getLicense.mockResolvedValue({ installed: true, state: 'grace', fingerprint: 'fp' })
    render(
      <LicenseProvider>
        <Probe />
      </LicenseProvider>,
    )
    expect(await screen.findByText('read-write')).toBeInTheDocument()
  })

  it('flips to read-only once the grace period has fully expired', async () => {
    getLicense.mockResolvedValue({ installed: true, state: 'expired_grace', fingerprint: 'fp' })
    render(
      <LicenseProvider>
        <Probe />
      </LicenseProvider>,
    )
    await waitFor(() => expect(screen.getByText('read-only')).toBeInTheDocument())
  })
})
