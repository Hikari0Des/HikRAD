import { render, screen } from '@testing-library/react'
import { beforeEach, describe, expect, it } from 'vitest'

import { I18nProvider } from '@hikrad/shared'
import en from '@hikrad/shared/locales/en/panel.json'

import type { Pool } from '../../api/types'
import { AuthProvider } from '../../auth/AuthContext'
import { tokenStore } from '../../auth/tokenStore'
import { ToastProvider } from '../../components/Toast'
import { ProfileBulkBar } from './ProfileBulkBar'

function renderBar(selectedIds: string[]) {
  return render(
    <I18nProvider>
      <AuthProvider>
        <ToastProvider>
          <ProfileBulkBar
            filter={{ archived: 'no' }}
            selectedIds={selectedIds}
            pools={[] as Pool[]}
            onDone={() => {}}
          />
        </ToastProvider>
      </AuthProvider>
    </I18nProvider>,
  )
}

beforeEach(() => {
  window.localStorage.clear()
  tokenStore.setManager({ id: 'm1', username: 'admin', role: 'admin' })
})

// FR-111.2: the bar must always say which target mode is live — the whole
// filter match set, or exactly the ticked rows — before an action is pressed.
describe('ProfileBulkBar scope line', () => {
  it('says "every matching plan" when nothing is selected', () => {
    renderBar([])
    expect(screen.getByText(en.profiles.bulk.scopeHint)).toBeInTheDocument()
  })

  it('says "N selected" when rows are ticked', () => {
    renderBar(['p1', 'p2'])
    expect(screen.getByText(en.bulk.scopeSelected.replace('{count}', '2'))).toBeInTheDocument()
    // And the archive action is offered (admin has profiles.edit).
    expect(screen.getByRole('button', { name: en.profiles.bulk.archive })).toBeInTheDocument()
  })
})
