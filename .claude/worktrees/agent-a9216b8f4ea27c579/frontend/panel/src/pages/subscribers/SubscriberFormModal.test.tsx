import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { I18nProvider } from '@hikrad/shared'
import en from '@hikrad/shared/locales/en/panel.json'

import { ToastProvider } from '../../components/Toast'
import { SubscriberFormModal } from './SubscriberFormModal'

const createSubscriber = vi.fn()
vi.mock('../../api/subscribers', () => ({
  createSubscriber: (body: unknown) => createSubscriber(body),
  updateSubscriber: vi.fn(),
}))
vi.mock('../../api/nas', () => ({
  listNas: () => Promise.resolve({ items: [] }),
}))

function renderModal(onSaved = vi.fn()) {
  return render(
    <I18nProvider>
      <ToastProvider>
        <SubscriberFormModal
          open
          onOpenChange={() => {}}
          profiles={[]}
          managers={[]}
          onSaved={onSaved}
        />
      </ToastProvider>
    </I18nProvider>,
  )
}

// FR-61/94, C5/C6: ServiceTypeRadio used to be an ungrouped, unnamed
// <input type="radio">; this locks the Radix RadioGroup rebuild's actual
// behavior — real radio semantics, and the chosen value reaching the save
// payload — rather than just checking it renders.
describe('SubscriberFormModal — service-type RadioGroup (FR-61, C5/C6)', () => {
  it('renders the service-type choices as real radios, not native inputs', () => {
    const { container } = renderModal()
    expect(container.querySelector('input[type="radio"]')).toBeNull()
    expect(screen.getAllByRole('radio')).toHaveLength(3)
  })

  it('defaults to pppoe and lets the operator pick a different service type', () => {
    renderModal()
    const pppoe = screen.getByRole('radio', { name: en.serviceType.pppoe })
    const hotspot = screen.getByRole('radio', { name: en.serviceType.hotspot })
    expect(pppoe).toBeChecked()
    expect(hotspot).not.toBeChecked()
    fireEvent.click(hotspot)
    expect(hotspot).toBeChecked()
    expect(pppoe).not.toBeChecked()
  })

  it('submits the selected service type', async () => {
    createSubscriber.mockResolvedValue({ id: 's1', service_type: 'hotspot' })
    const onSaved = vi.fn()
    renderModal(onSaved)

    fireEvent.change(screen.getByLabelText(en.subscriber.username), {
      target: { value: 'newuser' },
    })
    fireEvent.click(screen.getByRole('radio', { name: en.serviceType.hotspot }))
    fireEvent.click(screen.getByText(en.ui.save))

    await waitFor(() => expect(createSubscriber).toHaveBeenCalled())
    expect(createSubscriber.mock.calls[0][0]).toMatchObject({ service_type: 'hotspot' })
    expect(onSaved).toHaveBeenCalled()
  })
})

// FR-94, C1: the noPassword Checkbox kept its native-event-shaped onChange
// (checked, e.target.checked) so this call site needed zero changes when
// form.tsx became Radix-backed — this locks that it still actually works.
describe('SubscriberFormModal — noPassword Checkbox (C1)', () => {
  it('toggles and disables the password field', () => {
    renderModal()
    // The Checkbox's accessible name concatenates its label + description
    // text (both live inside the same <label>, unchanged from the
    // pre-modernization component) — match by substring, not exact text.
    const noPassword = screen.getByRole('checkbox', {
      name: (accName) => accName.startsWith(en.subscriber.noPassword),
    })
    const password = screen.getByLabelText(en.subscriber.password)
    expect(password).not.toBeDisabled()
    fireEvent.click(noPassword)
    expect(password).toBeDisabled()
  })
})
