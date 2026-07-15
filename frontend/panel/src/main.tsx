import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { BrowserRouter } from 'react-router-dom'

import { I18nProvider } from '@hikrad/shared'
import '@hikrad/shared/ui.css'

import { App } from './App'
import { AuthProvider } from './auth/AuthContext'
import { ErrorBoundary } from './components/ErrorBoundary'
import { ToastProvider } from './components/Toast'
import { BrandedManifestLink } from './pwa/BrandedManifestLink'
import { InstallBanner } from './pwa/InstallBanner'
import { NotificationClickRouter } from './pwa/NotificationClickRouter'
import { OfflineBanner } from './pwa/OfflineBanner'
import { registerServiceWorker } from './pwa/registerServiceWorker'
import { UpdateToast } from './pwa/UpdateToast'
import './index.css'

// PWA packaging (contract C5/FR-54) — Phase-4 cross-boundary exception, Agent
// F, Agent E unstaffed this phase (see frontend/panel/src/pwa/README.md).
// Everything below imports only from src/pwa/**; the tree/JSX otherwise is
// untouched from Phase 3.
createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <I18nProvider>
      <BrandedManifestLink />
      <OfflineBanner />
      <ErrorBoundary>
        <BrowserRouter>
          <AuthProvider>
            <ToastProvider>
              <NotificationClickRouter />
              <App />
            </ToastProvider>
          </AuthProvider>
        </BrowserRouter>
      </ErrorBoundary>
      <InstallBanner />
      <UpdateToast />
    </I18nProvider>
  </StrictMode>,
)

registerServiceWorker()
