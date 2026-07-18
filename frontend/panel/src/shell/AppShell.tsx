import { useState } from 'react'
import { Outlet } from 'react-router-dom'

import * as Dialog from '@radix-ui/react-dialog'

import { useT } from '@hikrad/shared'

import { LicenseBanner } from '../license/LicenseBanner'
import { LicenseProvider } from '../license/LicenseContext'
import { CriticalBanner, NotificationProvider } from './notifications'
import { PoweredByFooter } from './PoweredByFooter'
import { SidebarContent } from './Sidebar'
import { TopBar } from './TopBar'

/**
 * Panel layout: fixed sidebar from md up (inline-start, so it mirrors under
 * RTL), a Radix Dialog drawer below md (down to 360 px phone width — persona
 * Hassan), top bar with the global-search slot and the user menu.
 */
export function AppShell() {
  const [drawerOpen, setDrawerOpen] = useState(false)
  const t = useT()

  return (
    <LicenseProvider>
      <NotificationProvider>
        <div className="min-h-screen">
          <aside className="fixed inset-y-0 start-0 z-20 hidden w-64 border-e border-surface-sunken bg-surface-raised md:block">
            <SidebarContent />
          </aside>

          <Dialog.Root open={drawerOpen} onOpenChange={setDrawerOpen}>
            <Dialog.Portal>
              <Dialog.Overlay className="fixed inset-0 z-30 bg-ink/40 md:hidden" />
              <Dialog.Content
                aria-describedby={undefined}
                className="fixed inset-y-0 start-0 z-40 w-64 max-w-[80vw] bg-surface-raised shadow-xl md:hidden"
              >
                <Dialog.Title className="sr-only">{t('nav.menu')}</Dialog.Title>
                <Dialog.Close asChild>
                  <button
                    type="button"
                    aria-label={t('nav.close')}
                    className="absolute end-2 top-3 rounded-md p-2 hover:bg-surface-sunken"
                  >
                    <svg
                      aria-hidden="true"
                      width="16"
                      height="16"
                      viewBox="0 0 16 16"
                      fill="none"
                      stroke="currentColor"
                      strokeWidth="2"
                      strokeLinecap="round"
                    >
                      <path d="M3 3l10 10M13 3L3 13" />
                    </svg>
                  </button>
                </Dialog.Close>
                <SidebarContent onNavigate={() => setDrawerOpen(false)} />
              </Dialog.Content>
            </Dialog.Portal>
          </Dialog.Root>

          <div className="md:ps-64">
            <TopBar onOpenMenu={() => setDrawerOpen(true)} />
            <CriticalBanner />
            <LicenseBanner />
            <main className="mx-auto w-full max-w-6xl p-4 md:p-6">
              <Outlet />
            </main>
            <PoweredByFooter />
          </div>
        </div>
      </NotificationProvider>
    </LicenseProvider>
  )
}
