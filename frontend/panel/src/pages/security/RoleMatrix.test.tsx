import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { describe, expect, it } from 'vitest'

import { I18nProvider } from '@hikrad/shared'

import type { PermissionGroup } from '../../api/security'
import { normalise, RoleMatrix } from './RoleMatrix'

const catalog: PermissionGroup[] = [
  { module: 'subscribers', permissions: ['subscribers.view', 'subscribers.edit'] },
  { module: 'actions', permissions: ['renew', 'topup'] },
]

function Harness({ initial }: { initial: string[] }) {
  const [perms, setPerms] = useState<Set<string>>(new Set(initial))
  return (
    <I18nProvider>
      <RoleMatrix catalog={catalog} value={perms} onChange={setPerms} />
    </I18nProvider>
  )
}

describe('RoleMatrix (FR-27.1)', () => {
  it('normalise groups permissions into module × verb rows, bare perms under actions', () => {
    const rows = normalise(catalog)
    const subs = rows.find((r) => r.module === 'subscribers')
    expect(subs?.perms.map((p) => p.verb)).toEqual(['view', 'edit'])
    const actions = rows.find((r) => r.module === 'actions')
    expect(actions?.perms.map((p) => p.perm)).toEqual(['renew', 'topup'])
  })

  it('reflects the current set as checked boxes', () => {
    render(<Harness initial={['subscribers.view']} />)
    expect(screen.getByLabelText('subscribers.view')).toBeChecked()
    expect(screen.getByLabelText('subscribers.edit')).not.toBeChecked()
  })

  it('toggling a cell adds and removes the permission', async () => {
    const user = userEvent.setup()
    render(<Harness initial={['subscribers.view']} />)

    // Grant edit.
    await user.click(screen.getByLabelText('subscribers.edit'))
    expect(screen.getByLabelText('subscribers.edit')).toBeChecked()

    // Revoke view.
    await user.click(screen.getByLabelText('subscribers.view'))
    expect(screen.getByLabelText('subscribers.view')).not.toBeChecked()

    // A bare action permission toggles too.
    await user.click(screen.getByLabelText('actions.renew'))
    expect(screen.getByLabelText('actions.renew')).toBeChecked()
  })
})
